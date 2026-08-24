//go:build darwin && cgo

package player

/*
#cgo LDFLAGS: -framework CoreAudio
#include <CoreAudio/CoreAudio.h>
#include <stdint.h>

static unsigned int halpradio_prev_transport = 0;

static unsigned int halpradio_is_bluetooth(unsigned int t) {
    return t == kAudioDeviceTransportTypeBluetooth || t == kAudioDeviceTransportTypeBluetoothLE;
}

static unsigned int halpradio_current_transport(void) {
    AudioObjectPropertyAddress addr = {
        kAudioHardwarePropertyDefaultOutputDevice,
        kAudioObjectPropertyScopeGlobal,
        kAudioObjectPropertyElementMain
    };
    AudioObjectID deviceID = kAudioObjectUnknown;
    UInt32 size = sizeof(deviceID);
    if (AudioObjectGetPropertyData(kAudioObjectSystemObject, &addr, 0, NULL, &size, &deviceID) != noErr) {
        return 0;
    }
    AudioObjectPropertyAddress taddr = {
        kAudioDevicePropertyTransportType,
        kAudioObjectPropertyScopeGlobal,
        kAudioObjectPropertyElementMain
    };
    unsigned int transport = 0;
    size = sizeof(transport);
    if (AudioObjectGetPropertyData(deviceID, &taddr, 0, NULL, &size, &transport) != noErr) {
        return 0;
    }
    return transport;
}

extern void halpradioGoDefaultDeviceChanged(uint32_t leftBluetooth);

static OSStatus halpradio_default_device_changed(
    AudioObjectID inObjectID,
    UInt32 inNumberAddresses,
    const AudioObjectPropertyAddress inAddresses[],
    void *inClientData) {
    (void)inObjectID;
    (void)inNumberAddresses;
    (void)inAddresses;
    (void)inClientData;
    unsigned int cur = halpradio_current_transport();
    uint32_t leftBT = halpradio_is_bluetooth(halpradio_prev_transport) && !halpradio_is_bluetooth(cur);
    halpradio_prev_transport = cur;
    halpradioGoDefaultDeviceChanged(leftBT);
    return noErr;
}

static int halpradio_start_listener(void) {
    AudioObjectPropertyAddress addr = {
        kAudioHardwarePropertyDefaultOutputDevice,
        kAudioObjectPropertyScopeGlobal,
        kAudioObjectPropertyElementMain
    };
    halpradio_prev_transport = halpradio_current_transport();
    return (int)AudioObjectAddPropertyListener(kAudioObjectSystemObject, &addr, halpradio_default_device_changed, NULL);
}

static int halpradio_stop_listener(void) {
    AudioObjectPropertyAddress addr = {
        kAudioHardwarePropertyDefaultOutputDevice,
        kAudioObjectPropertyScopeGlobal,
        kAudioObjectPropertyElementMain
    };
    return (int)AudioObjectRemovePropertyListener(kAudioObjectSystemObject, &addr, halpradio_default_device_changed, NULL);
}
*/
import "C"

import (
	"sync"
)

var (
	autoPauseMu   sync.Mutex
	autoPauseCh   chan struct{}
	autoPauseStop func()
)

//export halpradioGoDefaultDeviceChanged
func halpradioGoDefaultDeviceChanged(leftBluetooth C.uint32_t) {
	if leftBluetooth == 0 {
		return
	}
	autoPauseMu.Lock()
	ch := autoPauseCh
	autoPauseMu.Unlock()
	if ch != nil {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// startAutoPause registers a CoreAudio listener on the default output device
// that invokes onEvent whenever the default device leaves a Bluetooth device
// (e.g. AirPods taken out of the ears). The returned function removes the
// listener. It returns nil when the listener could not be registered.
func startAutoPause(onEvent func()) func() {
	autoPauseMu.Lock()
	defer autoPauseMu.Unlock()
	if autoPauseStop != nil {
		return nil
	}
	if code := C.halpradio_start_listener(); code != 0 {
		return nil
	}
	ch := make(chan struct{}, 1)
	autoPauseCh = ch
	done := make(chan struct{})
	var once sync.Once
	stop := func() {
		once.Do(func() {
			close(done)
			C.halpradio_stop_listener()
			autoPauseMu.Lock()
			autoPauseCh = nil
			autoPauseStop = nil
			autoPauseMu.Unlock()
		})
	}
	autoPauseStop = stop

	go func() {
		for {
			select {
			case <-ch:
				onEvent()
			case <-done:
				return
			}
		}
	}()
	return stop
}
