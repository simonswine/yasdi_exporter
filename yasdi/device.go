package yasdi

/*
#cgo CFLAGS: -I/usr/include/libyasdi
#cgo LDFLAGS: -l:libyasdimaster.so.1
#include <libyasdimaster.h>
#include <chandef.h>
*/
import "C"

import (
	"encoding/binary"
	"fmt"
	"unsafe"

	"github.com/sirupsen/logrus"
)

type Device struct {
	log *logrus.Entry

	handle         uint
	Type           string
	Serial         uint
	channelHandles []uint
	channelNames   []string
	channelUnits   []string
}

func NewDevice(handle uint, yc *Connection) *Device {
	d := Device{
		handle: handle,
		log:    yc.log.WithField("device_handle", handle),
	}

	d.GetDeviceType()
	d.GetSerial()
	d.log.Data["serial"] = d.Serial
	return &d
}

func (d *Device) Log() *logrus.Entry {
	return d.log
}

func (d *Device) GetDeviceType() {
	buf := make([]byte, 32)
	ret_val := C.GetDeviceType(
		C.DWORD(d.handle),
		(*C.char)(unsafe.Pointer(&buf[0])),
		C.int(len(buf)),
	)
	if ret_val != 0 {
		d.log.Warnf(
			"Getting device type failed device_handle=%d ret_val=%d",
			d.handle,
			ret_val,
		)
	}
	d.Type = string(buf)
}

func (d *Device) GetSerial() {
	buf := make([]byte, 4)
	ret_val := C.GetDeviceSN(
		C.DWORD(d.handle),
		(*C.DWORD)(unsafe.Pointer(&buf[0])),
	)
	if ret_val != 0 {
		d.log.Warnf(
			"Getting device serial failed device_handle=%d ret_val=%d",
			d.handle,
			ret_val,
		)
	}
	d.Serial = uint(binary.LittleEndian.Uint32(buf))
}

func (d *Device) ChannelUnits() ([]string, error) {
	if len(d.channelUnits) == 0 {
		handles, err := d.ChannelHandles()
		if err != nil {
			return []string{}, err
		}

		output := make([]string, len(handles))
		for i := range handles {
			buf := make([]byte, 64)
			bufPtr := (*C.char)(unsafe.Pointer(&buf))
			retCode := C.GetChannelUnit(C.DWORD(handles[i]), bufPtr, C.DWORD(len(buf)-1))
			if retCode != 0 {
				return []string{}, fmt.Errorf("error retrieving channel units for device_handle=%d channel_handle=%d return_code=%d", d.handle, handles[i], retCode)
			}
			output[i] = C.GoString(bufPtr)
		}

		d.channelUnits = output
	}
	return d.channelUnits, nil
}

func (d *Device) ChannelNames() ([]string, error) {
	if len(d.channelNames) == 0 {
		handles, err := d.ChannelHandles()
		if err != nil {
			return []string{}, err
		}

		output := make([]string, len(handles))
		for i := range handles {
			buf := make([]byte, 64)
			bufPtr := (*C.char)(unsafe.Pointer(&buf))
			retCode := C.GetChannelName(C.DWORD(handles[i]), bufPtr, C.DWORD(len(buf)-1))
			if retCode != 0 {
				return []string{}, fmt.Errorf("error retrieving channel names for device_handle=%d channel_handle=%d return_code=%d", d.handle, handles[i], retCode)
			}
			output[i] = C.GoString(bufPtr)
		}

		d.channelNames = output
	}
	return d.channelNames, nil
}

func (d *Device) ChannelHandles() ([]uint, error) {
	if len(d.channelHandles) == 0 {
		var channel_handles [MAX_CHANNELS]C.DWORD

		retCode := C.GetChannelHandlesEx(
			C.DWORD(d.handle),
			(*C.DWORD)(unsafe.Pointer(&channel_handles)),
			C.DWORD(MAX_CHANNELS),
			C.SPOTCHANNELS,
		)

		if retCode < 1 || retCode > 128 {
			return []uint{}, fmt.Errorf("getting ChannelHandles failed device_handle=%d return_code=%d", d.handle, int(retCode))
		}

		output := make([]uint, retCode)
		for pos, _ := range output {
			output[pos] = uint(channel_handles[pos])
		}

		d.channelHandles = output
	}

	return d.channelHandles, nil
}

func (d *Device) GridVoltage() (float64, error) {
	value, _, err := d.ChannelValue("Uac")
	if err != nil {
		return 0, err
	}
	return value, nil
}

func (d *Device) GridPower() (float64, error) {
	value, _, err := d.ChannelValue("Pac")
	if err != nil {
		return 0, err
	}
	return value, nil
}

func (d *Device) GridFrequency() (float64, error) {
	value, _, err := d.ChannelValue("Fac")
	if err != nil {
		return 0, err
	}
	return value, nil
}

func (d *Device) TotalYield() (int64, error) {
	value, _, err := d.ChannelValue("E-Total")
	if err != nil {
		return 0, err
	}
	return int64(value * 1000), nil
}

func (d *Device) Status() (string, error) {
	_, value, err := d.ChannelValue("Status")
	if err != nil {
		return "", err
	}
	return value, nil
}

func (d *Device) ChannelValue(name string) (float64, string, error) {
	channelHandles, err := d.ChannelHandles()
	if err != nil {
		return 0.0, "", err
	}

	channelNames, err := d.ChannelNames()
	if err != nil {
		return 0.0, "", err
	}

	for pos, channelName := range channelNames {
		if name == channelName {
			return d.channelValueByHandle(channelHandles[pos])
		}
	}

	return 0.0, "", fmt.Errorf("error reading channel values with name=%s valid_names=%+v", name, channelNames)
}

func (d *Device) channelValueByHandle(handle uint) (float64, string, error) {
	maxValueAge := C.DWORD(10)
	var value C.double
	textValue := make([]byte, 64)
	textValuePtr := (*C.char)(unsafe.Pointer(&textValue))
	retCode := C.GetChannelValue(C.DWORD(handle), C.DWORD(d.handle), &value, textValuePtr, C.DWORD(len(textValue)-1), maxValueAge)
	if retCode != 0 {
		return 0.0, "", fmt.Errorf("error reading channel value, device_handle=%d channel_handle=%d return_code=%d", d.handle, handle, retCode)
	}

	return float64(value), C.GoString(textValuePtr), nil
}
