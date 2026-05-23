#ifndef MURRLY_UICONTEXT_H
#define MURRLY_UICONTEXT_H

// mur_focus_context carries the result of probing the focused UI
// element. Fields:
//   ok            — 1 if we have actionable data (either at_start or
//                   a real preceding char), 0 otherwise.
//   at_start      — 1 if cursor location == 0.
//   preceding     — UTF-16 code unit immediately before the cursor
//                   (valid only when ok == 1 and at_start == 0).
//   status        — diagnostic: which step succeeded last. See
//                   mur_focus_status_t below.
typedef enum {
    MUR_FOCUS_NO_SYSTEMWIDE   = 0,  // AXUIElementCreateSystemWide failed
    MUR_FOCUS_NO_FOCUSED      = 1,  // no focused UI element / AX refused
    MUR_FOCUS_NO_RANGE        = 2,  // focused element didn't expose selection range
    MUR_FOCUS_AT_START        = 3,  // got range, cursor at position 0
    MUR_FOCUS_VALUE_OK        = 4,  // got range + read full value, preceding extracted
    MUR_FOCUS_PARAM_OK        = 5,  // value failed but parameterized substring fallback worked
    MUR_FOCUS_NO_VALUE        = 6,  // got range but neither value nor parameterized read worked
} mur_focus_status_t;

typedef struct {
    int          ok;
    int          at_start;
    unsigned int preceding;
    int          status;
} mur_focus_context;

mur_focus_context mur_read_focus_context(void);

#endif
