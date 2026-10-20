//go:build windows

// Windows screen saver (.scr) with a DVD-style bouncing logo.
//
//	/s           run full screen (one window per monitor)
//	/p <hwnd>    render inside the preview box of the Screen Saver Settings dialog
//	/c, (none)   show the settings dialog (there are no settings)
package main

import (
	"bytes"
	_ "embed"
	"image"
	"image/draw"
	"image/png"
	"math"
	"math/rand"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

//go:embed logo.png
var logoPNG []byte

const (
	logoWidthFraction = 0.35 // logo width relative to the monitor width
	speedFraction     = 0.09 // pixels per second relative to the longer monitor side
	frameMillis       = 15
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	winmm    = syscall.NewLazyDLL("winmm.dll")

	pRegisterClassExW    = user32.NewProc("RegisterClassExW")
	pCreateWindowExW     = user32.NewProc("CreateWindowExW")
	pDefWindowProcW      = user32.NewProc("DefWindowProcW")
	pDestroyWindow       = user32.NewProc("DestroyWindow")
	pGetMessageW         = user32.NewProc("GetMessageW")
	pTranslateMessage    = user32.NewProc("TranslateMessage")
	pDispatchMessageW    = user32.NewProc("DispatchMessageW")
	pPostQuitMessage     = user32.NewProc("PostQuitMessage")
	pGetClientRect       = user32.NewProc("GetClientRect")
	pGetDC               = user32.NewProc("GetDC")
	pReleaseDC           = user32.NewProc("ReleaseDC")
	pBeginPaint          = user32.NewProc("BeginPaint")
	pEndPaint            = user32.NewProc("EndPaint")
	pSetTimer            = user32.NewProc("SetTimer")
	pKillTimer           = user32.NewProc("KillTimer")
	pShowCursor          = user32.NewProc("ShowCursor")
	pLoadCursorW         = user32.NewProc("LoadCursorW")
	pMessageBoxW         = user32.NewProc("MessageBoxW")
	pEnumDisplayMonitors = user32.NewProc("EnumDisplayMonitors")
	pGetMonitorInfoW     = user32.NewProc("GetMonitorInfoW")
	pSetProcessDPIAware  = user32.NewProc("SetProcessDPIAware")
	pSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	pGetCursorPos        = user32.NewProc("GetCursorPos")
	pCreateCompatibleDC  = gdi32.NewProc("CreateCompatibleDC")
	pCreateDIBSection    = gdi32.NewProc("CreateDIBSection")
	pSelectObject        = gdi32.NewProc("SelectObject")
	pDeleteObject        = gdi32.NewProc("DeleteObject")
	pDeleteDC            = gdi32.NewProc("DeleteDC")
	pBitBlt              = gdi32.NewProc("BitBlt")
	pGetModuleHandleW    = kernel32.NewProc("GetModuleHandleW")
	pTimeBeginPeriod     = winmm.NewProc("timeBeginPeriod")
	pTimeEndPeriod       = winmm.NewProc("timeEndPeriod")
)

const (
	wsPopup       = 0x80000000
	wsChild       = 0x40000000
	wsVisible     = 0x10000000
	wsExTopmost   = 0x00000008
	wsExToolWin   = 0x00000080
	wmDestroy     = 0x0002
	wmPaint       = 0x000F
	wmClose       = 0x0010
	wmEraseBkgnd  = 0x0014
	wmActivateApp = 0x001C
	wmSetCursor   = 0x0020
	wmKeyDown     = 0x0100
	wmSysKeyDown  = 0x0104
	wmTimer       = 0x0113
	wmMouseMove   = 0x0200
	wmLButtonDown = 0x0201
	wmRButtonDown = 0x0204
	wmMButtonDown = 0x0207
	wmMouseWheel  = 0x020A
	csHRedraw     = 0x0002
	csVRedraw     = 0x0001
	srccopy       = 0x00CC0020
	idcArrow      = 32512
	mbIconInfo    = 0x00000040
)

type point struct{ X, Y int32 }
type rect struct{ Left, Top, Right, Bottom int32 }

type msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
	private uint32
}

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type paintStruct struct {
	Hdc       uintptr
	Erase     int32
	Paint     rect
	Restore   int32
	IncUpdate int32
	Reserved  [32]byte
}

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type monitorInfo struct {
	Size    uint32
	Monitor rect
	Work    rect
	Flags   uint32
}

// saver is the state of one bouncing logo inside one window.
type saver struct {
	hwnd     uintptr
	w, h     int
	memDC    uintptr
	dib      uintptr
	oldBmp   uintptr
	pixels   []uint32 // top-down BGRA frame buffer, always black except the logo
	logo     []uint32 // premultiplied BGRA logo scaled for this window
	lw, lh   int
	x, y     float64
	vx, vy   float64
	drawnX   int
	drawnY   int
	lastTick time.Time
}

var (
	savers      = map[uintptr]*saver{}
	fullscreen  bool
	srcLogo     *image.NRGBA
	firstMouse  = true
	mouseOrigin point
	closing     bool
)

func init() { runtime.LockOSThread() }

func main() {
	pSetProcessDPIAware.Call()

	mode, arg := parseArgs(os.Args[1:])
	switch mode {
	case 's':
		fullscreen = true
	case 'p':
		if arg == 0 {
			return
		}
	default:
		msgBox(0, "Cyberpunk Edgerunners II screen saver\n\nThis screen saver has no settings.", "Edgerunners II")
		return
	}

	img, err := png.Decode(bytes.NewReader(logoPNG))
	if err != nil {
		msgBox(0, "Failed to load logo: "+err.Error(), "Edgerunners II")
		return
	}
	srcLogo = image.NewNRGBA(img.Bounds())
	draw.Draw(srcLogo, srcLogo.Bounds(), img, img.Bounds().Min, draw.Src)

	hInst, _, _ := pGetModuleHandleW.Call(0)
	className := utf16("EdgeRunners2Saver")
	cursor, _, _ := pLoadCursorW.Call(0, idcArrow)
	wc := wndClassEx{
		Style:     csHRedraw | csVRedraw,
		WndProc:   syscall.NewCallback(wndProc),
		Instance:  hInst,
		Cursor:    cursor,
		ClassName: className,
	}
	wc.Size = uint32(unsafe.Sizeof(wc))
	pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	pTimeBeginPeriod.Call(1)
	defer pTimeEndPeriod.Call(1)

	if fullscreen {
		var monitors []rect
		cb := syscall.NewCallback(func(hMon, hdc, lprc, data uintptr) uintptr {
			mi := monitorInfo{}
			mi.Size = uint32(unsafe.Sizeof(mi))
			pGetMonitorInfoW.Call(hMon, uintptr(unsafe.Pointer(&mi)))
			monitors = append(monitors, mi.Monitor)
			return 1
		})
		pEnumDisplayMonitors.Call(0, 0, cb, 0)
		for _, m := range monitors {
			hwnd, _, _ := pCreateWindowExW.Call(wsExTopmost|wsExToolWin,
				uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(utf16("Edgerunners II"))),
				wsPopup|wsVisible,
				uintptr(m.Left), uintptr(m.Top), uintptr(m.Right-m.Left), uintptr(m.Bottom-m.Top),
				0, 0, hInst, 0)
			if hwnd != 0 {
				pSetForegroundWindow.Call(hwnd)
			}
		}
		pShowCursor.Call(0)
	} else {
		var r rect
		pGetClientRect.Call(arg, uintptr(unsafe.Pointer(&r)))
		pCreateWindowExW.Call(0,
			uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(utf16("Preview"))),
			wsChild|wsVisible, 0, 0, uintptr(r.Right-r.Left), uintptr(r.Bottom-r.Top),
			arg, 0, hInst, 0)
	}

	var m msg
	for {
		ret, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(ret) <= 0 {
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	if fullscreen {
		pShowCursor.Call(1)
	}
}

// parseArgs accepts the forms Windows uses: "/s", "/p 1234", "/p:1234", "/c:1234", "-S".
func parseArgs(args []string) (byte, uintptr) {
	if len(args) == 0 {
		return 'c', 0
	}
	a := strings.ToLower(strings.TrimLeft(args[0], "/-"))
	if a == "" {
		return 'c', 0
	}
	mode := a[0]
	rest := strings.TrimLeft(a[1:], ":")
	if rest == "" && len(args) > 1 {
		rest = args[1]
	}
	n, _ := strconv.ParseUint(rest, 10, 64)
	return mode, uintptr(n)
}

func wndProc(hwnd, message, wParam, lParam uintptr) uintptr {
	switch message {
	case 0x0001: // WM_CREATE
		s := newSaver(hwnd)
		savers[hwnd] = s
		pSetTimer.Call(hwnd, 1, frameMillis, 0)
		return 0
	case wmTimer:
		if s := savers[hwnd]; s != nil {
			s.step()
		}
		return 0
	case wmEraseBkgnd:
		return 1
	case wmPaint:
		var ps paintStruct
		hdc, _, _ := pBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		if s := savers[hwnd]; s != nil {
			pBitBlt.Call(hdc, 0, 0, uintptr(s.w), uintptr(s.h), s.memDC, 0, 0, srccopy)
		}
		pEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		return 0
	case wmSetCursor:
		if fullscreen {
			return 1
		}
	case wmMouseMove:
		if fullscreen {
			var p point
			pGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
			if firstMouse {
				firstMouse, mouseOrigin = false, p
			} else if abs(p.X-mouseOrigin.X) > 8 || abs(p.Y-mouseOrigin.Y) > 8 {
				quitAll()
			}
			return 0
		}
	case wmKeyDown, wmSysKeyDown, wmLButtonDown, wmRButtonDown, wmMButtonDown, wmMouseWheel:
		if fullscreen {
			quitAll()
			return 0
		}
	case wmActivateApp:
		if fullscreen && wParam == 0 {
			quitAll()
		}
	case wmClose:
		if fullscreen {
			quitAll()
			return 0
		}
	case wmDestroy:
		pKillTimer.Call(hwnd, 1)
		if s := savers[hwnd]; s != nil {
			s.free()
			delete(savers, hwnd)
		}
		if len(savers) == 0 {
			pPostQuitMessage.Call(0)
		}
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, message, wParam, lParam)
	return r
}

func quitAll() {
	if closing {
		return
	}
	closing = true
	for h := range savers {
		pDestroyWindow.Call(h)
	}
}

func newSaver(hwnd uintptr) *saver {
	var r rect
	pGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	s := &saver{hwnd: hwnd, w: max(1, int(r.Right-r.Left)), h: max(1, int(r.Bottom-r.Top))}

	hdc, _, _ := pGetDC.Call(hwnd)
	s.memDC, _, _ = pCreateCompatibleDC.Call(hdc)
	pReleaseDC.Call(hwnd, hdc)
	bi := bitmapInfoHeader{Width: int32(s.w), Height: -int32(s.h), Planes: 1, BitCount: 32}
	bi.Size = uint32(unsafe.Sizeof(bi))
	var bits unsafe.Pointer
	s.dib, _, _ = pCreateDIBSection.Call(s.memDC, uintptr(unsafe.Pointer(&bi)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	s.oldBmp, _, _ = pSelectObject.Call(s.memDC, s.dib)
	s.pixels = unsafe.Slice((*uint32)(bits), s.w*s.h)
	clear(s.pixels)

	sw := srcLogo.Bounds().Dx()
	sh := srcLogo.Bounds().Dy()
	s.lw = max(1, int(math.Round(float64(s.w)*logoWidthFraction)))
	s.lh = max(1, s.lw*sh/sw)
	if s.lh > s.h*8/10 {
		s.lh = max(1, s.h*8/10)
		s.lw = max(1, s.lh*sw/sh)
	}
	s.logo = resizePremultiplied(srcLogo, s.lw, s.lh)

	s.x = rand.Float64() * float64(max(0, s.w-s.lw))
	s.y = rand.Float64() * float64(max(0, s.h-s.lh))
	speed := float64(max(s.w, s.h)) * speedFraction
	angle := math.Pi/6 + rand.Float64()*math.Pi/6
	s.vx = speed * math.Cos(angle)
	s.vy = speed * math.Sin(angle)
	if rand.Intn(2) == 0 {
		s.vx = -s.vx
	}
	if rand.Intn(2) == 0 {
		s.vy = -s.vy
	}
	s.drawnX, s.drawnY = int(s.x), int(s.y)
	s.blitLogo(s.drawnX, s.drawnY)
	s.lastTick = time.Now()
	return s
}

func (s *saver) free() {
	pSelectObject.Call(s.memDC, s.oldBmp)
	pDeleteObject.Call(s.dib)
	pDeleteDC.Call(s.memDC)
	s.pixels = nil
}

func (s *saver) step() {
	now := time.Now()
	dt := math.Min(now.Sub(s.lastTick).Seconds(), 0.1)
	s.lastTick = now

	maxX := float64(max(0, s.w-s.lw))
	maxY := float64(max(0, s.h-s.lh))
	s.x += s.vx * dt
	s.y += s.vy * dt
	if s.x <= 0 {
		s.x, s.vx = -s.x, math.Abs(s.vx)
	}
	if s.x >= maxX {
		s.x, s.vx = 2*maxX-s.x, -math.Abs(s.vx)
	}
	if s.y <= 0 {
		s.y, s.vy = -s.y, math.Abs(s.vy)
	}
	if s.y >= maxY {
		s.y, s.vy = 2*maxY-s.y, -math.Abs(s.vy)
	}
	s.x = math.Max(0, math.Min(s.x, maxX))
	s.y = math.Max(0, math.Min(s.y, maxY))

	nx, ny := int(math.Round(s.x)), int(math.Round(s.y))
	if nx == s.drawnX && ny == s.drawnY {
		return
	}
	ox, oy := s.drawnX, s.drawnY
	s.clearLogo(ox, oy)
	s.blitLogo(nx, ny)
	s.drawnX, s.drawnY = nx, ny

	// Copy only the union of the old and new logo rectangles to the screen.
	x0, y0 := min(ox, nx), min(oy, ny)
	x1, y1 := min(s.w, max(ox, nx)+s.lw), min(s.h, max(oy, ny)+s.lh)
	hdc, _, _ := pGetDC.Call(s.hwnd)
	pBitBlt.Call(hdc, uintptr(x0), uintptr(y0), uintptr(x1-x0), uintptr(y1-y0), s.memDC, uintptr(x0), uintptr(y0), srccopy)
	pReleaseDC.Call(s.hwnd, hdc)
}

func (s *saver) clearLogo(x, y int) {
	for row := 0; row < s.lh && y+row < s.h; row++ {
		start := (y+row)*s.w + x
		clear(s.pixels[start : start+min(s.lw, s.w-x)])
	}
}

// blitLogo writes the premultiplied logo onto the black frame buffer, which is
// exactly "source over black".
func (s *saver) blitLogo(x, y int) {
	for row := 0; row < s.lh && y+row < s.h; row++ {
		start := (y+row)*s.w + x
		n := min(s.lw, s.w-x)
		copy(s.pixels[start:start+n], s.logo[row*s.lw:row*s.lw+n])
	}
}

// resizePremultiplied scales with a separable triangle filter (area-averaging
// when shrinking) and returns premultiplied 0x00RRGGBB pixels.
func resizePremultiplied(src *image.NRGBA, dw, dh int) []uint32 {
	sw, sh := src.Bounds().Dx(), src.Bounds().Dy()
	pre := make([][4]float64, sw*sh)
	for y := 0; y < sh; y++ {
		for x := 0; x < sw; x++ {
			i := src.PixOffset(x, y)
			a := float64(src.Pix[i+3]) / 255
			pre[y*sw+x] = [4]float64{float64(src.Pix[i]) * a, float64(src.Pix[i+1]) * a, float64(src.Pix[i+2]) * a, a}
		}
	}
	tmp := make([][4]float64, dw*sh)
	wx := filterWeights(sw, dw)
	for y := 0; y < sh; y++ {
		for x := 0; x < dw; x++ {
			var acc [4]float64
			for _, t := range wx[x] {
				p := pre[y*sw+t.i]
				for c := 0; c < 4; c++ {
					acc[c] += p[c] * t.w
				}
			}
			tmp[y*dw+x] = acc
		}
	}
	out := make([]uint32, dw*dh)
	wy := filterWeights(sh, dh)
	for y := 0; y < dh; y++ {
		for x := 0; x < dw; x++ {
			var acc [4]float64
			for _, t := range wy[y] {
				p := tmp[t.i*dw+x]
				for c := 0; c < 4; c++ {
					acc[c] += p[c] * t.w
				}
			}
			r := uint32(math.Max(0, math.Min(255, acc[0]+.5)))
			g := uint32(math.Max(0, math.Min(255, acc[1]+.5)))
			b := uint32(math.Max(0, math.Min(255, acc[2]+.5)))
			out[y*dw+x] = r<<16 | g<<8 | b
		}
	}
	return out
}

type tap struct {
	i int
	w float64
}

func filterWeights(srcN, dstN int) [][]tap {
	scale := float64(dstN) / float64(srcN)
	support := math.Max(1, 1/scale)
	res := make([][]tap, dstN)
	for d := 0; d < dstN; d++ {
		center := (float64(d)+.5)/scale - .5
		lo := int(math.Floor(center - support))
		hi := int(math.Ceil(center + support))
		var taps []tap
		sum := 0.0
		for i := lo; i <= hi; i++ {
			w := 1 - math.Abs(float64(i)-center)/support
			if w <= 0 {
				continue
			}
			taps = append(taps, tap{max(0, min(srcN-1, i)), w})
			sum += w
		}
		for k := range taps {
			taps[k].w /= sum
		}
		res[d] = taps
	}
	return res
}

func msgBox(owner uintptr, text, title string) {
	pMessageBoxW.Call(owner, uintptr(unsafe.Pointer(utf16(text))), uintptr(unsafe.Pointer(utf16(title))), mbIconInfo)
}

func utf16(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

func abs(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}
