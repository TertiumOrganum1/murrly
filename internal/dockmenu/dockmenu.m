#import <Cocoa/Cocoa.h>
#include "dockmenu.h"

typedef void (*mur_callback)(void);

@interface MurrlyDockMenuDelegate : NSObject <NSApplicationDelegate> {
@public
    id<NSApplicationDelegate> originalDelegate;
    NSMenu* dockMenu;
    mur_callback cbQuit;
    mur_callback cbOpenConfig;
    mur_callback cbCopyLatest;
}
- (NSMenu*)applicationDockMenu:(NSApplication*)sender;
- (void)didPickQuit:(id)sender;
- (void)didPickOpenConfig:(id)sender;
- (void)didPickCopyLatest:(id)sender;
@end

@implementation MurrlyDockMenuDelegate

// Chain unknown selectors to the original (systray-installed) delegate so
// we don't break anything systray relies on.
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

- (void)didPickQuit:(id)sender {
    if (cbQuit) cbQuit();
}
- (void)didPickOpenConfig:(id)sender {
    if (cbOpenConfig) cbOpenConfig();
}
- (void)didPickCopyLatest:(id)sender {
    if (cbCopyLatest) cbCopyLatest();
}
@end

static MurrlyDockMenuDelegate* gMurDelegate = nil;

void mur_dockmenu_install(
    void (*onQuit)(void),
    void (*onOpenConfig)(void),
    void (*onCopyLatest)(void)
) {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (gMurDelegate) return; // idempotent

        gMurDelegate = [[MurrlyDockMenuDelegate alloc] init];
        gMurDelegate->cbQuit = onQuit;
        gMurDelegate->cbOpenConfig = onOpenConfig;
        gMurDelegate->cbCopyLatest = onCopyLatest;
        gMurDelegate->originalDelegate = [NSApp delegate];

        NSMenu* menu = [[NSMenu alloc] init];
        [menu setAutoenablesItems:NO];

        NSMenuItem* copyItem = [[NSMenuItem alloc]
            initWithTitle:@"Copy latest transcript"
            action:@selector(didPickCopyLatest:)
            keyEquivalent:@""];
        [copyItem setTarget:gMurDelegate];
        [menu addItem:copyItem];

        NSMenuItem* openItem = [[NSMenuItem alloc]
            initWithTitle:@"Open config file"
            action:@selector(didPickOpenConfig:)
            keyEquivalent:@""];
        [openItem setTarget:gMurDelegate];
        [menu addItem:openItem];

        [menu addItem:[NSMenuItem separatorItem]];

        NSMenuItem* quitItem = [[NSMenuItem alloc]
            initWithTitle:@"Quit Murrly"
            action:@selector(didPickQuit:)
            keyEquivalent:@""];
        [quitItem setTarget:gMurDelegate];
        [menu addItem:quitItem];

        gMurDelegate->dockMenu = menu;

        [NSApp setDelegate:gMurDelegate];
    });
}
