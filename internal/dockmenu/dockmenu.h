#ifndef MURRLY_DOCKMENU_H
#define MURRLY_DOCKMENU_H

// Install builds the Dock right-click menu:
//   * Copy-transcript slots (indices 0..2 = latest, previous, older)
//   * Reload Config
//   * Open Config
//   * Model submenu (modelLabels[N], picked by index)
//   * Autostart toggle
//   * Permissions submenu (Microphone / Accessibility — open System Settings)
//   * Quit
//
// modelLabels is a NULL-terminated array of UTF-8 C strings. modelCount
// must match the array length excluding the terminator.
void mur_dockmenu_install(
    void (*onCopyTranscript)(int index),
    void (*onPickModel)(int index),
    void (*onToggleAutostart)(void),
    void (*onOpenConfig)(void),
    void (*onReloadConfig)(void),
    void (*onOpenMicSettings)(void),
    void (*onOpenAccessibility)(void),
    void (*onQuit)(void),
    const char* const* modelLabels,
    int modelCount
);

void mur_dockmenu_set_transcripts(const char* latest, const char* previous, const char* older);
void mur_dockmenu_set_autostart(int enabled);
void mur_dockmenu_set_model_index(int index); // marks the active model with a checkmark

#endif
