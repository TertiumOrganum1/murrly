#ifndef MURRLY_DOCKMENU_H
#define MURRLY_DOCKMENU_H

// mur_dockmenu_install sets up a Dock right-click menu with a small set
// of items. Callbacks are invoked when the user picks the item.
void mur_dockmenu_install(
    void (*onQuit)(void),
    void (*onOpenConfig)(void),
    void (*onCopyLatest)(void)
);

#endif
