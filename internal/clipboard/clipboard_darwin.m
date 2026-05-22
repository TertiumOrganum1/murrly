#import <Cocoa/Cocoa.h>
#include <stdlib.h>
#include <string.h>

#include "clipboard_darwin.h"

int mur_clip_write_text(const char* utf8) {
    if (utf8 == NULL) return -1;
    NSString* s = [NSString stringWithUTF8String:utf8];
    if (s == nil) return -2;
    NSPasteboard* pb = [NSPasteboard generalPasteboard];
    [pb clearContents];
    BOOL ok = [pb setString:s forType:NSPasteboardTypeString];
    return ok ? 0 : -3;
}

void* mur_clip_save_state(void) {
    NSPasteboard* pb = [NSPasteboard generalPasteboard];
    NSArray<NSPasteboardItem*>* items = [pb pasteboardItems];
    if (items == nil || [items count] == 0) {
        return NULL;
    }
    // Deep-copy every type of every item so the token survives subsequent
    // clearContents calls. NSPasteboardItem holds data lazily by default;
    // we materialise it now to detach from the live pasteboard.
    NSMutableArray<NSPasteboardItem*>* copy = [NSMutableArray arrayWithCapacity:[items count]];
    for (NSPasteboardItem* item in items) {
        NSPasteboardItem* dup = [[NSPasteboardItem alloc] init];
        for (NSPasteboardType type in [item types]) {
            NSData* data = [item dataForType:type];
            if (data) {
                [dup setData:data forType:type];
            }
        }
        [copy addObject:dup];
    }
    // Bridge into a CF-retained pointer so it survives across the Go
    // callsite. mur_clip_restore_state / mur_clip_free_state owns it now.
    return (void*)CFBridgingRetain(copy);
}

void mur_clip_restore_state(void* token) {
    NSPasteboard* pb = [NSPasteboard generalPasteboard];
    if (token == NULL) {
        [pb clearContents];
        return;
    }
    NSArray<NSPasteboardItem*>* items = (NSArray*)CFBridgingRelease(token);
    [pb clearContents];
    [pb writeObjects:items];
}

void mur_clip_free_state(void* token) {
    if (token == NULL) return;
    (void)CFBridgingRelease(token);
}
