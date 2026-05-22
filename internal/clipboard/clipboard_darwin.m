#import <Cocoa/Cocoa.h>
#import <os/log.h>
#include <stdlib.h>
#include <string.h>

#include "clipboard_darwin.h"

// os_log subsystem for inspecting clipboard activity if anything ever
// goes wrong. Inspect with:
//   log stream --predicate 'subsystem == "com.tertiumorganum1.murrly"' --info
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
    if (!ok) {
        os_log_error(mur_log(), "write_text: setString failed (len=%lu)",
            (unsigned long)[s length]);
        return -3;
    }
    return 0;
}

void* mur_clip_save_state(void) {
    NSPasteboard* pb = [NSPasteboard generalPasteboard];
    NSArray<NSPasteboardItem*>* items = [pb pasteboardItems];
    if (items == nil || [items count] == 0) {
        return NULL;
    }
    // Deep-copy every type of every item so the token survives subsequent
    // clearContents calls. dataWithBytes:length: forces a fresh independent
    // buffer — without it, NSData backed by the pasteboard's owned memory
    // becomes invalid after clearContents.
    NSMutableArray<NSPasteboardItem*>* copy = [NSMutableArray arrayWithCapacity:[items count]];
    for (NSPasteboardItem* item in items) {
        NSPasteboardItem* dup = [[NSPasteboardItem alloc] init];
        for (NSPasteboardType type in [item types]) {
            NSData* data = [item dataForType:type];
            if (!data) continue;
            NSData* detached = [NSData dataWithBytes:[data bytes] length:[data length]];
            [dup setData:detached forType:type];
        }
        [copy addObject:dup];
    }
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
    BOOL ok = [pb writeObjects:items];
    if (!ok) {
        os_log_error(mur_log(), "restore_state: writeObjects failed for %lu items",
            (unsigned long)[items count]);
    }
}

void mur_clip_free_state(void* token) {
    if (token == NULL) return;
    (void)CFBridgingRelease(token);
}
