#ifndef MURRLY_OVERLAY_H
#define MURRLY_OVERLAY_H

#include <stddef.h>

// mur_overlay_show shows the pill with the given UTF-8 label and an
// optional icon. iconData may be NULL (no icon) or a PNG byte buffer;
// iconLen is the buffer length. The icon is rendered as a template
// image tinted white to match the pill's dark background.
void mur_overlay_show(const unsigned char* iconData, size_t iconLen, const char* utf8);
void mur_overlay_hide(void);

#endif
