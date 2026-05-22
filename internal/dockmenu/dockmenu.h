#ifndef MURRLY_DOCKMENU_H
#define MURRLY_DOCKMENU_H

// Install builds the Dock right-click menu with Copy-transcript slots
// (indices 0, 1, 2 = latest, previous, older) plus Open Config and Quit.
void mur_dockmenu_install(
    void (*onCopyTranscript)(int index),
    void (*onOpenConfig)(void),
    void (*onQuit)(void)
);

// Update the user-visible titles of the three transcript items. Pass
// NULL or empty string to disable a slot (it will appear greyed out).
// Safe to call from any thread.
void mur_dockmenu_set_transcripts(const char* latest, const char* previous, const char* older);

#endif
