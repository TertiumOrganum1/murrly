//go:build darwin

#import <Cocoa/Cocoa.h>
#import <QuartzCore/QuartzCore.h>

#include "overlay.h"

// NSTextFieldCell subclass that vertically centers its drawn text inside
// the cell bounds — NSTextField defaults to top-aligned rendering and
// looks visually off in our pill layout.
@interface MurrlyCenteredTextFieldCell : NSTextFieldCell
@end

@implementation MurrlyCenteredTextFieldCell
- (NSRect)titleRectForBounds:(NSRect)theRect {
    NSRect titleRect = [super titleRectForBounds:theRect];
    NSSize titleSize = [[self attributedStringValue] size];
    CGFloat dy = (theRect.size.height - titleSize.height) / 2.0;
    titleRect.origin.y += dy;
    titleRect.size.height -= dy;
    return titleRect;
}
- (void)drawInteriorWithFrame:(NSRect)cellFrame inView:(NSView*)controlView {
    [super drawInteriorWithFrame:[self titleRectForBounds:cellFrame] inView:controlView];
}
@end

@interface MurrlyOverlay : NSObject {
    NSWindow* window;
    NSImageView* iconView;
    NSTextField* label;
}
- (void)ensureWindow;
- (void)showIcon:(NSImage*)icon text:(NSString*)text;
- (void)hideOverlay;
@end

@implementation MurrlyOverlay
- (void)ensureWindow {
    if (window) return;

    NSRect frame = NSMakeRect(0, 0, 240, 36);
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

    iconView = [[NSImageView alloc] initWithFrame:NSMakeRect(12, 8, 20, 20)];
    if (@available(macOS 10.14, *)) {
        iconView.contentTintColor = [NSColor whiteColor];
    }
    [iconView setImageScaling:NSImageScaleProportionallyDown];
    [root addSubview:iconView];

    // Use the full pill height for the label and rely on the custom cell
    // to vertically center the text.
    label = [[NSTextField alloc] initWithFrame:NSMakeRect(38, 0, 192, 36)];
    MurrlyCenteredTextFieldCell* cell = [[MurrlyCenteredTextFieldCell alloc] init];
    [cell setBezeled:NO];
    [cell setDrawsBackground:NO];
    [cell setEditable:NO];
    [cell setSelectable:NO];
    [cell setTextColor:[NSColor whiteColor]];
    [cell setFont:[NSFont systemFontOfSize:14 weight:NSFontWeightMedium]];
    [cell setAlignment:NSTextAlignmentLeft];
    [label setCell:cell];
    [root addSubview:label];
}

- (void)showIcon:(NSImage*)icon text:(NSString*)text {
    [self ensureWindow];

    // Layout constants.
    const CGFloat padX = 14.0;          // left/right padding inside the pill
    const CGFloat iconSize = 20.0;
    const CGFloat iconGap = 8.0;        // space between icon and label
    const CGFloat pillHeight = 36.0;

    // Measure text width using the same attributes as the label.
    NSFont* font = [[label cell] font];
    NSDictionary* attrs = @{NSFontAttributeName: font};
    NSSize textSize = [text sizeWithAttributes:attrs];
    CGFloat textW = ceil(textSize.width) + 2.0; // small fudge so descenders don't clip

    CGFloat contentW;
    if (icon) {
        contentW = iconSize + iconGap + textW;
    } else {
        contentW = textW;
    }
    CGFloat pillW = padX + contentW + padX;

    if (icon) {
        [icon setTemplate:YES];
        [iconView setImage:icon];
        [iconView setHidden:NO];
        [iconView setFrame:NSMakeRect(padX, (pillHeight - iconSize) / 2.0, iconSize, iconSize)];
        [label setFrame:NSMakeRect(padX + iconSize + iconGap, 0, textW, pillHeight)];
        [[label cell] setAlignment:NSTextAlignmentLeft];
    } else {
        [iconView setHidden:YES];
        [label setFrame:NSMakeRect(padX, 0, textW, pillHeight)];
        [[label cell] setAlignment:NSTextAlignmentCenter];
    }
    [label setStringValue:text];
    [label setNeedsDisplay:YES];

    // Resize the window to fit, then center horizontally below the menu bar.
    NSScreen* screen = [NSScreen mainScreen];
    NSRect screenFrame = [screen visibleFrame];
    CGFloat x = NSMidX(screenFrame) - pillW / 2.0;
    CGFloat y = NSMaxY(screenFrame) - pillHeight - 6.0;
    [window setFrame:NSMakeRect(x, y, pillW, pillHeight) display:YES];

    [window orderFrontRegardless];
}

- (void)hideOverlay {
    if (!window) return;
    [window orderOut:nil];
}
@end

static MurrlyOverlay* gMurOverlay = nil;

void mur_overlay_show(const unsigned char* iconData, size_t iconLen, const char* utf8) {
    NSString* text = utf8 ? [NSString stringWithUTF8String:utf8] : @"";
    NSImage* icon = nil;
    if (iconData && iconLen > 0) {
        NSData* data = [NSData dataWithBytes:iconData length:iconLen];
        icon = [[NSImage alloc] initWithData:data];
    }
    dispatch_async(dispatch_get_main_queue(), ^{
        if (!gMurOverlay) gMurOverlay = [[MurrlyOverlay alloc] init];
        [gMurOverlay showIcon:icon text:text];
    });
}

void mur_overlay_hide(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (gMurOverlay) [gMurOverlay hideOverlay];
    });
}
