#ifndef MURRLY_DOCKMENU_H
#define MURRLY_DOCKMENU_H

// Install builds the Dock right-click menu with Copy-transcript slots
// (indices 0..2 = latest, previous, older), an autostart toggle, plus
// Open Config and Quit.
void mur_dockmenu_install(
    void (*onCopyTranscript)(int index),
    void (*onToggleAutostart)(void),
    void (*onOpenConfig)(void),
    void (*onQuit)(void)
);

// Update the user-visible titles of the three transcript items. Pass
// NULL or empty string to disable a slot (it will appear greyed out).
void mur_dockmenu_set_transcripts(const char* latest, const char* previous, const char* older);

// Set the checkmark on the autostart toggle. Pass 1 for enabled, 0 for disabled.
void mur_dockmenu_set_autostart(int enabled);

#endif
