//go:build darwin

#import <Cocoa/Cocoa.h>
#import <ApplicationServices/ApplicationServices.h>
#include "uicontext_darwin.h"

// mur_read_focus_context queries the frontmost app's focused UI
// element for what's immediately to the left of the cursor.
//
// We use NSWorkspace.frontmostApplication + AXUIElementCreateApplication
// instead of AXUIElementCreateSystemWide because the system-wide
// handle's kAXFocusedUIElementAttribute is unreliable in practice —
// returns kAXErrorAttributeUnsupported in many real-world setups
// (this is why Raycast / Karabiner / similar all go through the
// frontmost-PID path).
//
// Webviews / Electron-based fields (VS Code panels, Slack,
// Discord, ...) commonly expose AXSelectedTextRange but refuse
// AXValue. For those we fall back to
// AXStringForRangeParameterizedAttribute, which reads just one
// character at (location-1, 1) — many fields implement that even
// when the bulk-value attribute is blocked.
//
// If both reads fail, status == NO_VALUE and ok == 0; Apply then
// passes the recognised text through unchanged.
mur_focus_context mur_read_focus_context(void) {
    mur_focus_context ctx = {0, 0, 0, MUR_FOCUS_NO_FOCUSED};

    NSRunningApplication *frontmost = [[NSWorkspace sharedWorkspace] frontmostApplication];
    if (frontmost == nil) {
        return ctx;
    }
    pid_t pid = [frontmost processIdentifier];
    AXUIElementRef app = AXUIElementCreateApplication(pid);
    if (app == NULL) {
        return ctx;
    }

    AXUIElementRef focused = NULL;
    AXError err = AXUIElementCopyAttributeValue(
        app,
        kAXFocusedUIElementAttribute,
        (CFTypeRef *)&focused);
    CFRelease(app);
    if (err != kAXErrorSuccess || focused == NULL) {
        return ctx;
    }

    CFTypeRef rangeRef = NULL;
    err = AXUIElementCopyAttributeValue(
        focused,
        kAXSelectedTextRangeAttribute,
        &rangeRef);
    if (err != kAXErrorSuccess || rangeRef == NULL) {
        CFRelease(focused);
        ctx.status = MUR_FOCUS_NO_RANGE;
        return ctx;
    }

    CFRange range = {0, 0};
    Boolean rangeOk = AXValueGetValue((AXValueRef)rangeRef, kAXValueCFRangeType, &range);
    CFRelease(rangeRef);
    if (!rangeOk) {
        CFRelease(focused);
        ctx.status = MUR_FOCUS_NO_RANGE;
        return ctx;
    }

    if (range.location == 0) {
        CFRelease(focused);
        ctx.ok = 1;
        ctx.at_start = 1;
        ctx.status = MUR_FOCUS_AT_START;
        return ctx;
    }

    // Attempt 1: full value attribute. Cheapest path when supported.
    CFTypeRef valueRef = NULL;
    err = AXUIElementCopyAttributeValue(focused, kAXValueAttribute, &valueRef);
    if (err == kAXErrorSuccess && valueRef != NULL &&
        CFGetTypeID(valueRef) == CFStringGetTypeID()) {
        CFStringRef str = (CFStringRef)valueRef;
        CFIndex len = CFStringGetLength(str);
        if (range.location > 0 && range.location <= len) {
            UniChar ch = CFStringGetCharacterAtIndex(str, range.location - 1);
            ctx.preceding = (unsigned int)ch;
            ctx.ok = 1;
            ctx.status = MUR_FOCUS_VALUE_OK;
        }
        CFRelease(valueRef);
        if (ctx.ok) {
            CFRelease(focused);
            return ctx;
        }
    } else if (valueRef != NULL) {
        CFRelease(valueRef);
    }

    // Attempt 2: parameterized read of the single char at (location-1, 1).
    // Webviews and other AX-quirky containers often implement this even
    // when the full kAXValueAttribute read is blocked.
    CFRange probeRange = CFRangeMake(range.location - 1, 1);
    AXValueRef probeRangeAX = AXValueCreate(kAXValueCFRangeType, &probeRange);
    if (probeRangeAX != NULL) {
        CFTypeRef paramRef = NULL;
        err = AXUIElementCopyParameterizedAttributeValue(
            focused,
            kAXStringForRangeParameterizedAttribute,
            probeRangeAX,
            &paramRef);
        CFRelease(probeRangeAX);
        if (err == kAXErrorSuccess && paramRef != NULL &&
            CFGetTypeID(paramRef) == CFStringGetTypeID() &&
            CFStringGetLength((CFStringRef)paramRef) >= 1) {
            UniChar ch = CFStringGetCharacterAtIndex((CFStringRef)paramRef, 0);
            ctx.preceding = (unsigned int)ch;
            ctx.ok = 1;
            ctx.status = MUR_FOCUS_PARAM_OK;
            CFRelease(paramRef);
            CFRelease(focused);
            return ctx;
        }
        if (paramRef != NULL) {
            CFRelease(paramRef);
        }
    }

    CFRelease(focused);
    ctx.status = MUR_FOCUS_NO_VALUE;
    return ctx;
}
