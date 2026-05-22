#ifndef MURRLY_CLIPBOARD_DARWIN_H
#define MURRLY_CLIPBOARD_DARWIN_H

#include <stddef.h>

// Write the given UTF-8 string as the sole NSPasteboard content.
// Atomic — competing readers see the full new state immediately.
// Returns 0 on success, negative on failure.
int mur_clip_write_text(const char* utf8);

// Snapshot the current pasteboard. The returned token (an opaque handle)
// preserves every type (text, image, RTF, file URLs, etc.) of every
// NSPasteboardItem currently on the board. Returns NULL if the board is
// empty. Caller MUST eventually call mur_clip_restore_state or
// mur_clip_free_state to release the token.
void* mur_clip_save_state(void);

// Restore the pasteboard from a previously-saved snapshot. Consumes the
// token (frees it). Pass NULL to clear the board instead.
void mur_clip_restore_state(void* token);

// Free a snapshot token without restoring it. Use when the snapshot is
// no longer needed.
void mur_clip_free_state(void* token);

#endif
