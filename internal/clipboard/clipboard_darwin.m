#import <Cocoa/Cocoa.h>
#import <os/log.h>
#include <stdlib.h>
#include <string.h>

#include "clipboard_darwin.h"

static os_log_t mur_log(void) {
    static os_log_t log;
    static dispatch_once_t once;
    dispatch_once(&once, ^{
        log = os_log_create("com.tertiumorganum1.murrly", "clipboard");
    });
    return log;
}

int mur_clip_write_text(const char* utf8) {
    if (utf8 == NULL) return -1;
    NSString* s = [NSString stringWithUTF8String:utf8];
    if (s == nil) return -2;
    NSPasteboard* pb = [NSPasteboard generalPasteboard];
    [pb clearContents];
    BOOL ok = [pb setString:s forType:NSPasteboardTypeString];
    os_log(mur_log(), "write_text: %{public}s (len=%lu)",
        ok ? "OK" : "FAIL", (unsigned long)[s length]);
    return ok ? 0 : -3;
}

void* mur_clip_save_state(void) {
    NSPasteboard* pb = [NSPasteboard generalPasteboard];
    NSArray<NSPasteboardItem*>* items = [pb pasteboardItems];
    os_log(mur_log(), "save_state: pasteboard has %{public}lu items",
        (unsigned long)[items count]);
    if (items == nil || [items count] == 0) {
        return NULL;
    }
    NSMutableArray<NSPasteboardItem*>* copy = [NSMutableArray arrayWithCapacity:[items count]];
    for (NSPasteboardItem* item in items) {
        NSPasteboardItem* dup = [[NSPasteboardItem alloc] init];
        NSArray<NSPasteboardType>* types = [item types];
        os_log(mur_log(), "save_state: item has %{public}lu types: %{public}@",
            (unsigned long)[types count], types);
        for (NSPasteboardType type in types) {
            NSData* data = [item dataForType:type];
            if (!data) {
                os_log(mur_log(), "  type=%{public}@ → nil data, skipping", type);
                continue;
            }
            NSData* detached = [NSData dataWithBytes:[data bytes] length:[data length]];
            BOOL ok = [dup setData:detached forType:type];
            os_log(mur_log(), "  type=%{public}@ bytes=%{public}lu setData=%{public}s",
                type, (unsigned long)[detached length], ok ? "OK" : "FAIL");
        }
        [copy addObject:dup];
    }
    os_log(mur_log(), "save_state: returning token with %{public}lu items",
        (unsigned long)[copy count]);
    return (void*)CFBridgingRetain(copy);
}

void mur_clip_restore_state(void* token) {
    NSPasteboard* pb = [NSPasteboard generalPasteboard];
    if (token == NULL) {
        os_log(mur_log(), "restore_state: token=NULL → clearContents only");
        [pb clearContents];
        return;
    }
    NSArray<NSPasteboardItem*>* items = (NSArray*)CFBridgingRelease(token);
    os_log(mur_log(), "restore_state: writing %{public}lu items back",
        (unsigned long)[items count]);
    for (NSPasteboardItem* item in items) {
        os_log(mur_log(), "  item types to write: %{public}@", [item types]);
    }
    [pb clearContents];
    BOOL ok = [pb writeObjects:items];
    os_log(mur_log(), "restore_state: writeObjects=%{public}s",
        ok ? "OK" : "FAIL");
    NSArray* after = [pb pasteboardItems];
    os_log(mur_log(), "restore_state: pasteboard now has %{public}lu items",
        (unsigned long)[after count]);
    for (NSPasteboardItem* item in after) {
        os_log(mur_log(), "  item types now: %{public}@", [item types]);
    }
}

void mur_clip_free_state(void* token) {
    if (token == NULL) return;
    (void)CFBridgingRelease(token);
}
