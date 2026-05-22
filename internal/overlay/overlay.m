#import <Cocoa/Cocoa.h>
#import <QuartzCore/QuartzCore.h>

#include "overlay.h"

@interface MurrlyOverlay : NSObject {
    NSWindow* window;
    NSTextField* label;
}
- (void)ensureWindow;
- (void)showWithText:(NSString*)text;
- (void)hideOverlay;
@end

@implementation MurrlyOverlay
- (void)ensureWindow {
    if (window) return;

    NSRect frame = NSMakeRect(0, 0, 220, 36);
    window = [[NSWindow alloc]
        initWithContentRect:frame
        styleMask:NSWindowStyleMaskBorderless
        backing:NSBackingStoreBuffered
        defer:NO];
    [window setLevel:NSStatusWindowLevel];
    [window setOpaque:NO];
    [window setBackgroundColor:[NSColor clearColor]];
    [window setIgnoresMouseEvents:YES];
    [window setHasShadow:YES];
    [window setCollectionBehavior:
        NSWindowCollectionBehaviorCanJoinAllSpaces |
        NSWindowCollectionBehaviorStationary |
        NSWindowCollectionBehaviorIgnoresCycle |
        NSWindowCollectionBehaviorFullScreenAuxiliary];

    NSView* root = [window contentView];
    root.wantsLayer = YES;
    CALayer* layer = root.layer;
    layer.backgroundColor = [[NSColor colorWithCalibratedWhite:0.0 alpha:0.78] CGColor];
    layer.cornerRadius = 18.0;
    layer.masksToBounds = YES;

    label = [[NSTextField alloc] initWithFrame:NSMakeRect(0, 8, 220, 22)];
    [label setBezeled:NO];
    [label setDrawsBackground:NO];
    [label setEditable:NO];
    [label setSelectable:NO];
    [label setAlignment:NSTextAlignmentCenter];
    [label setTextColor:[NSColor whiteColor]];
    [label setFont:[NSFont systemFontOfSize:14 weight:NSFontWeightMedium]];
    [root addSubview:label];
}

- (void)showWithText:(NSString*)text {
    [self ensureWindow];
    [label setStringValue:text];

    NSScreen* screen = [NSScreen mainScreen];
    NSRect screenFrame = [screen visibleFrame];
    NSRect winFrame = [window frame];
    CGFloat x = NSMidX(screenFrame) - winFrame.size.width / 2.0;
    CGFloat y = NSMaxY(screenFrame) - winFrame.size.height - 6.0;
    [window setFrameOrigin:NSMakePoint(x, y)];

    [window orderFrontRegardless];
}

- (void)hideOverlay {
    if (!window) return;
    [window orderOut:nil];
}
@end

static MurrlyOverlay* gMurOverlay = nil;

void mur_overlay_show(const char* utf8) {
    NSString* text = utf8 ? [NSString stringWithUTF8String:utf8] : @"";
    dispatch_async(dispatch_get_main_queue(), ^{
        if (!gMurOverlay) gMurOverlay = [[MurrlyOverlay alloc] init];
        [gMurOverlay showWithText:text];
    });
}

void mur_overlay_hide(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (gMurOverlay) [gMurOverlay hideOverlay];
    });
}
