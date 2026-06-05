#ifndef MURRLY_DOCKMENU_H
#define MURRLY_DOCKMENU_H

// Install builds the Dock right-click menu:
//   * Copy-transcript slots (indices 0..2 = latest, previous, older)
//   * Reload Config
//   * Open Config
//   * Model submenu (modelLabels[N], picked by index)
//   * Scoring submenu (scoringLabels[N], picked by index) — how the best
//     multi-inference variant is chosen. Omitted when scoringCount < 2.
//   * Autostart toggle
//   * Permissions submenu (Microphone / Accessibility — open System Settings)
//   * Quit
//
// modelLabels / scoringLabels are arrays of UTF-8 C strings; modelCount /
// scoringCount must match their lengths.
void mur_dockmenu_install(
    void (*onCopyTranscript)(int index),
    void (*onPickModel)(int index),
    void (*onPickScoring)(int index),
    void (*onToggleMulti)(void),
    void (*onToggleAutostart)(void),
    void (*onOpenConfig)(void),
    void (*onReloadConfig)(void),
    void (*onOpenMicSettings)(void),
    void (*onOpenAccessibility)(void),
    void (*onReprocess)(void),
    void (*onQuit)(void),
    const char* const* modelLabels,
    int modelCount,
    const char* const* scoringLabels,
    int scoringCount,
    int multiEnabled
);

void mur_dockmenu_set_transcripts(const char* latest, const char* previous, const char* older);
void mur_dockmenu_set_autostart(int enabled);
void mur_dockmenu_set_multi(int enabled);       // checks the "multi-inference" toggle
void mur_dockmenu_set_model_index(int index);   // marks the active model with a checkmark
void mur_dockmenu_set_scoring_index(int index); // marks the active scoring mode with a checkmark

#endif
