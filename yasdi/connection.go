package yasdi

/*
#cgo CFLAGS: -I/usr/include/libyasdi
#cgo LDFLAGS: -l:libyasdimaster.so.1
#include <libyasdimaster.h>
#include <chandef.h>
*/
import "C"

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"github.com/sirupsen/logrus"
)

type Connection struct {
	log *logrus.Entry

	configFilePath string
	configPath     string
	State          uint // uninitialized 0, initialized 1
	DeviceHandles  []Device
}

type ConnectionSettings struct {
	Device   string
	Protocol string
	Baudrate string
	Media    string
}

func NewConnection(log *logrus.Entry, settings *ConnectionSettings) (*Connection, error) {
	configPath, err := ioutil.TempDir("", "libyasdi")
	if err != nil {
		return nil, err
	}

	// create devices folder
	if err := os.Mkdir(filepath.Join(configPath, "devices"), 0755); err != nil {
		return nil, err
	}

	// write channel list for our device
	if err := ioutil.WriteFile(filepath.Join(configPath, "devices", "WR20-004.bin"), yasdiDevicesWr20004Bin, 0644); err != nil {
		return nil, err
	}

	content := []byte(fmt.Sprintf(
		yasdiINI,
		settings.Device,
		settings.Media,
		settings.Baudrate,
		settings.Protocol,
		configPath,
	))

	configFilePath := filepath.Join(configPath, "yasdi.ini")
	if err := ioutil.WriteFile(configFilePath, content, 0644); err != nil {
		return nil, err
	}

	return &Connection{
		log:            log,
		configPath:     configPath,
		configFilePath: configFilePath,
		State:          0,
	}, nil
}

func (yc *Connection) Initialize() error {
	// driver sub system count
	var bus_driver_id C.DWORD
	var bus_inc C.DWORD

	// check for config file
	if _, err := os.Stat(yc.configFilePath); os.IsNotExist(err) {
		return fmt.Errorf("no such config file: %s", yc.configFilePath)
	}

	// switch working directory to temp
	if err := os.Chdir(yc.configPath); err != nil {
		return fmt.Errorf("unable to switch working directory to: %s", yc.configPath)
	}
	yc.log.WithField("working_dir", yc.configPath).Debug("switched working directory")

	ret_val := C.yasdiMasterInitialize(C.CString(yc.configFilePath), &bus_driver_id)
	if ret_val != 0 {
		return fmt.Errorf("error initializing YASDI master ret_val=%d", ret_val)
	}
	yc.log.Debugf("initialized YASDI master ret_val=%d", ret_val)
	yc.State |= 1

	// init bus drivers
	yc.log.Debugf("initialize available YASDI bus drivers")
	if bus_driver_id == 0 {
		yc.log.Warnf("no YASDI bus driver was found")
	}

	for bus_inc = 0; bus_inc < bus_driver_id; bus_inc++ {
		ret_val := C.yasdiMasterSetDriverOnline(bus_inc)
		yc.log.Debugf("initialized bus driver #%d ret_val=%d", bus_inc, ret_val)
	}

	return nil
}

func (yc *Connection) DetectDevices(device_count int, stopCh chan struct{}) error {

	ret_val := C.DoStartDeviceDetection(C.int(device_count), C.BOOL(0))
	yc.log.Debugf("started detection for %d device(s) ret_val=%d", device_count, ret_val)

	tries := 0
	max_tries := 50

	for true {

		handles, err := yc.findDeviceHandles()
		if err != nil {
			return err
		}

		if len(handles) >= device_count {
			yc.initDevices(handles)
			yc.log.Debugf("found %d device(s): %+v", len(handles), yc.DeviceHandles)
			return nil
		}

		if tries > max_tries {
			if len(handles) == 0 {
				return fmt.Errorf("No device found")
			} else {
				yc.log.Warnf("Only %d device(s) out of %d expected found", len(handles), device_count)
				yc.initDevices(handles)
				return nil
			}
		}

		select {
		case <-time.Tick(100 * time.Millisecond):
			break
		case <-stopCh:
			return errors.New("detection terminated")
		}
		tries += 1
	}

	return nil

}

func (yc *Connection) initDevices(handles []uint) error {
	for i := range handles {
		yc.DeviceHandles = append(yc.DeviceHandles, *NewDevice(handles[i], yc))
	}
	return nil
}

func (yc *Connection) findDeviceHandles() ([]uint, error) {
	var device_handles [MAX_INVERTERS]C.DWORD
	var output []uint

	ret_val := C.GetDeviceHandles(&(device_handles[0]), C.DWORD(MAX_INVERTERS))
	yc.log.Debugf("got available device handles ret_val=%d", ret_val)

	for i := range device_handles {
		if device_handles[i] != 0 {
			output = append(output, uint(device_handles[i]))
		}
	}
	yc.log.Debugf("found %d device handles %+v", len(output), output)

	return output, nil
}

func (yc *Connection) Shutdown() error {
	if yc.State != 1 {
		return nil
	}
	C.yasdiMasterShutdown()
	yc.State = 0
	yc.log.Debugf("shutted down YASDI")

	if err := os.RemoveAll(yc.configPath); err != nil {
		return err
	}

	return nil
}
