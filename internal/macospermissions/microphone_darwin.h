#ifndef MURRLY_MICROPHONE_H
#define MURRLY_MICROPHONE_H

// mur_microphone_authorization_status returns the current TCC decision
// for the Microphone service, mirroring AVAuthorizationStatus:
//   0 = NotDetermined (no prompt has been answered yet)
//   1 = Restricted    (MDM / parental controls)
//   2 = Denied
//   3 = Authorized
//
// Triggering the prompt is *not* done here — that happens through the
// real recording stack (CoreAudio AUHAL via PortAudio) so macOS sees
// the same client both times. See internal/recorder.Probe.
int mur_microphone_authorization_status(void);

#endif
