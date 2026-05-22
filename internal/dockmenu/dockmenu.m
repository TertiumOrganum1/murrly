#import <Cocoa/Cocoa.h>
#include "dockmenu.h"

typedef void (*mur_copy_cb)(int index);
typedef void (*mur_void_cb)(void);

@interface MurrlyDockMenuDelegate : NSObject <NSApplicationDelegate> {
@public
    id<NSApplicationDelegate> originalDelegate;
    NSMenu* dockMenu;
    NSMenuItem* copyItems[3];
    NSMenuItem* autostartItem;
    mur_copy_cb cbCopy;
    mur_void_cb cbToggleAutostart;
    mur_void_cb cbOpenConfig;
    mur_void_cb cbQuit;
}
- (NSMenu*)applicationDockMenu:(NSApplication*)sender;
- (void)didPickCopy0:(id)sender;
- (void)didPickCopy1:(id)sender;
- (void)didPickCopy2:(id)sender;
- (void)didPickAutostart:(id)sender;
- (void)didPickOpenConfig:(id)sender;
- (void)didPickQuit:(id)sender;
@end

@implementation MurrlyDockMenuDelegate

- (BOOL)respondsToSelector:(SEL)aSelector {
    if ([super respondsToSelector:aSelector]) return YES;
    return [originalDelegate respondsToSelector:aSelector];
}
- (id)forwardingTargetForSelector:(SEL)aSelector {
    if ([originalDelegate respondsToSelector:aSelector]) return originalDelegate;
    return nil;
}

- (NSMenu*)applicationDockMenu:(NSApplication*)sender {
    return dockMenu;
}

- (void)didPickCopy0:(id)sender { if (cbCopy) cbCopy(0); }
- (void)didPickCopy1:(id)sender { if (cbCopy) cbCopy(1); }
- (void)didPickCopy2:(id)sender { if (cbCopy) cbCopy(2); }
- (void)didPickAutostart:(id)sender { if (cbToggleAutostart) cbToggleAutostart(); }
- (void)didPickOpenConfig:(id)sender { if (cbOpenConfig) cbOpenConfig(); }
- (void)didPickQuit:(id)sender { if (cbQuit) cbQuit(); }

@end

static MurrlyDockMenuDelegate* gMurDelegate = nil;

// Placeholder shown when no transcript exists for that slot yet.
static NSString* emptySlotTitle(int idx) {
    switch (idx) {
        case 0: return @"— (последнее)";
        case 1: return @"— (предыдущее)";
        case 2: return @"— (ещё раньше)";
        default: return @"";
    }
}

void mur_dockmenu_install(
    void (*onCopyTranscript)(int index),
    void (*onToggleAutostart)(void),
    void (*onOpenConfig)(void),
    void (*onQuit)(void)
) {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (gMurDelegate) return;

        gMurDelegate = [[MurrlyDockMenuDelegate alloc] init];
        gMurDelegate->cbCopy = onCopyTranscript;
        gMurDelegate->cbToggleAutostart = onToggleAutostart;
        gMurDelegate->cbOpenConfig = onOpenConfig;
        gMurDelegate->cbQuit = onQuit;
        gMurDelegate->originalDelegate = [NSApp delegate];

        NSMenu* menu = [[NSMenu alloc] init];
        [menu setAutoenablesItems:NO];

        SEL copySelectors[3] = {
            @selector(didPickCopy0:),
            @selector(didPickCopy1:),
            @selector(didPickCopy2:)
        };
        for (int i = 0; i < 3; i++) {
            NSMenuItem* it = [[NSMenuItem alloc]
                initWithTitle:emptySlotTitle(i)
                action:copySelectors[i]
                keyEquivalent:@""];
            [it setTarget:gMurDelegate];
            [it setEnabled:NO]; // until a transcript exists for that slot
            [menu addItem:it];
            gMurDelegate->copyItems[i] = it;
        }

        [menu addItem:[NSMenuItem separatorItem]];

        NSMenuItem* autoItem = [[NSMenuItem alloc]
            initWithTitle:@"Запускать при логине"
            action:@selector(didPickAutostart:)
            keyEquivalent:@""];
        [autoItem setTarget:gMurDelegate];
        [autoItem setState:NSControlStateValueOff];
        [menu addItem:autoItem];
        gMurDelegate->autostartItem = autoItem;

        NSMenuItem* openItem = [[NSMenuItem alloc]
            initWithTitle:@"Открыть конфиг"
            action:@selector(didPickOpenConfig:)
            keyEquivalent:@""];
        [openItem setTarget:gMurDelegate];
        [menu addItem:openItem];

        [menu addItem:[NSMenuItem separatorItem]];

        NSMenuItem* quitItem = [[NSMenuItem alloc]
            initWithTitle:@"Завершить Murrly"
            action:@selector(didPickQuit:)
            keyEquivalent:@""];
        [quitItem setTarget:gMurDelegate];
        [menu addItem:quitItem];

        gMurDelegate->dockMenu = menu;

        [NSApp setDelegate:gMurDelegate];
    });
}

static NSString* truncatedPreview(NSString* full, NSUInteger limit) {
    if ([full length] <= limit) return full;
    return [[full substringToIndex:limit] stringByAppendingString:@"…"];
}

void mur_dockmenu_set_autostart(int enabled) {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (!gMurDelegate || !gMurDelegate->autostartItem) return;
        [gMurDelegate->autostartItem setState:
            enabled ? NSControlStateValueOn : NSControlStateValueOff];
    });
}

void mur_dockmenu_set_transcripts(const char* latest, const char* previous, const char* older) {
    const char* inputs[3] = {latest, previous, older};
    NSMutableArray* titles = [NSMutableArray arrayWithCapacity:3];
    NSMutableArray* enabledFlags = [NSMutableArray arrayWithCapacity:3];
    for (int i = 0; i < 3; i++) {
        if (inputs[i] && inputs[i][0] != '\0') {
            NSString* text = [NSString stringWithUTF8String:inputs[i]];
            NSString* clean = [text stringByTrimmingCharactersInSet:[NSCharacterSet whitespaceAndNewlineCharacterSet]];
            // Show the transcript fragment alone — clipboard semantics are
            // obvious from context and the user asked for fragments only.
            [titles addObject:truncatedPreview(clean, 56)];
            [enabledFlags addObject:@YES];
        } else {
            [titles addObject:emptySlotTitle(i)];
            [enabledFlags addObject:@NO];
        }
    }
    dispatch_async(dispatch_get_main_queue(), ^{
        if (!gMurDelegate) return;
        for (int i = 0; i < 3; i++) {
            NSMenuItem* item = gMurDelegate->copyItems[i];
            if (!item) continue;
            [item setTitle:titles[i]];
            [item setEnabled:[enabledFlags[i] boolValue]];
        }
    });
}
