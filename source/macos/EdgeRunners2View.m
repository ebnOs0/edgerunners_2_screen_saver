#import <ScreenSaver/ScreenSaver.h>
#import <QuartzCore/QuartzCore.h>

static const CGFloat kLogoWidthFraction = 0.35;  // logo width relative to the screen width
static const CGFloat kSpeedFraction     = 0.09;  // pixels per second relative to the longer screen side

@interface EdgeRunners2View : ScreenSaverView
@end

@implementation EdgeRunners2View {
    CALayer *_logoLayer;
    NSSize _logoSize;
    NSPoint _position;
    NSPoint _velocity;
    CFTimeInterval _lastTime;
}

- (instancetype)initWithFrame:(NSRect)frame isPreview:(BOOL)isPreview {
    self = [super initWithFrame:frame isPreview:isPreview];
    if (!self) return nil;

    [self setAnimationTimeInterval:1.0 / 60.0];
    self.wantsLayer = YES;

    NSBundle *bundle = [NSBundle bundleForClass:[self class]];
    NSImage *image = [[NSImage alloc] initWithContentsOfURL:[bundle URLForImageResource:@"logo"]];
    CGImageRef cgImage = [image CGImageForProposedRect:NULL context:nil hints:nil];

    _logoLayer = [CALayer layer];
    _logoLayer.contents = (__bridge id)cgImage;
    _logoLayer.contentsGravity = kCAGravityResize;
    _logoLayer.minificationFilter = kCAFilterTrilinear;
    _logoLayer.magnificationFilter = kCAFilterLinear;
    _logoLayer.anchorPoint = CGPointZero;
    _logoSize = cgImage ? NSMakeSize(CGImageGetWidth(cgImage), CGImageGetHeight(cgImage)) : NSMakeSize(4, 1);

    if (!isPreview) {
        // macOS 14+ runs savers inside legacyScreenSaver, which never tears the view down;
        // quitting on "willstop" keeps it from animating invisibly in the background.
        [[NSDistributedNotificationCenter defaultCenter] addObserverForName:@"com.apple.screensaver.willstop"
                                                                     object:nil
                                                                      queue:nil
                                                                 usingBlock:^(NSNotification *note) {
            dispatch_after(dispatch_time(DISPATCH_TIME_NOW, (int64_t)(0.5 * NSEC_PER_SEC)),
                           dispatch_get_main_queue(), ^{ exit(0); });
        }];
    }
    return self;
}

- (CALayer *)makeBackingLayer {
    CALayer *layer = [CALayer layer];
    layer.backgroundColor = CGColorGetConstantColor(kCGColorBlack);
    return layer;
}

- (void)layoutLogoResettingPosition:(BOOL)reset {
    NSRect b = self.bounds;
    if (b.size.width <= 0 || b.size.height <= 0) return;

    CGFloat w = round(b.size.width * kLogoWidthFraction);
    CGFloat h = round(w * _logoSize.height / _logoSize.width);
    if (h > b.size.height * 0.8) {
        h = round(b.size.height * 0.8);
        w = round(h * _logoSize.width / _logoSize.height);
    }

    [CATransaction begin];
    [CATransaction setDisableActions:YES];
    if (_logoLayer.superlayer != self.layer) [self.layer addSublayer:_logoLayer];
    _logoLayer.contentsScale = self.window.backingScaleFactor ?: 2.0;
    _logoLayer.bounds = CGRectMake(0, 0, w, h);
    [CATransaction commit];

    if (reset) {
        _position = NSMakePoint(SSRandomFloatBetween(0, MAX(0, b.size.width - w)),
                                SSRandomFloatBetween(0, MAX(0, b.size.height - h)));
        CGFloat speed = MAX(b.size.width, b.size.height) * kSpeedFraction;
        CGFloat angle = SSRandomFloatBetween(M_PI / 6, M_PI / 3);  // 30°..60° like the DVD logo
        _velocity = NSMakePoint(speed * cos(angle) * (SSRandomIntBetween(0, 1) ? 1 : -1),
                                speed * sin(angle) * (SSRandomIntBetween(0, 1) ? 1 : -1));
    }
    [self applyPosition];
}

- (void)applyPosition {
    [CATransaction begin];
    [CATransaction setDisableActions:YES];
    _logoLayer.position = CGPointMake(round(_position.x), round(_position.y));
    [CATransaction commit];
}

- (void)setFrameSize:(NSSize)newSize {
    [super setFrameSize:newSize];
    [self layoutLogoResettingPosition:YES];
}

- (void)viewDidChangeBackingProperties {
    [super viewDidChangeBackingProperties];
    _logoLayer.contentsScale = self.window.backingScaleFactor ?: 2.0;
}

- (void)startAnimation {
    [super startAnimation];
    [self layoutLogoResettingPosition:YES];
    _lastTime = CACurrentMediaTime();
}

- (void)animateOneFrame {
    CFTimeInterval now = CACurrentMediaTime();
    CGFloat dt = MIN(now - _lastTime, 0.1);
    _lastTime = now;

    NSRect b = self.bounds;
    CGSize s = _logoLayer.bounds.size;
    CGFloat maxX = MAX(0, b.size.width - s.width);
    CGFloat maxY = MAX(0, b.size.height - s.height);

    _position.x += _velocity.x * dt;
    _position.y += _velocity.y * dt;
    if (_position.x <= 0)    { _position.x = -_position.x;            _velocity.x =  fabs(_velocity.x); }
    if (_position.x >= maxX) { _position.x = 2 * maxX - _position.x;  _velocity.x = -fabs(_velocity.x); }
    if (_position.y <= 0)    { _position.y = -_position.y;            _velocity.y =  fabs(_velocity.y); }
    if (_position.y >= maxY) { _position.y = 2 * maxY - _position.y;  _velocity.y = -fabs(_velocity.y); }
    _position.x = MIN(MAX(_position.x, 0), maxX);
    _position.y = MIN(MAX(_position.y, 0), maxY);

    [self applyPosition];
}

- (BOOL)hasConfigureSheet {
    return NO;
}

- (NSWindow *)configureSheet {
    return nil;
}

@end
