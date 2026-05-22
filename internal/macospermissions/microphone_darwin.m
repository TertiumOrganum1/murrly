//go:build darwin

#import <AVFoundation/AVFoundation.h>
#include "microphone_darwin.h"

int mur_microphone_authorization_status(void) {
    return (int)[AVCaptureDevice authorizationStatusForMediaType:AVMediaTypeAudio];
}
