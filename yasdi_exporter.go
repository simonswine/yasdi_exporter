package main

import (
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/simonswine/yasdi_exporter/influxdb"
	"github.com/simonswine/yasdi_exporter/metrics"
	"github.com/simonswine/yasdi_exporter/yasdi"
)

var rootCmd = &cobra.Command{
	Use:   "yasdi_exporter",
	Short: "yasdi_exporter reads inverter metrics using libyasdi and makes them accessible to prometheus/influxdb",
	RunE: func(cmd *cobra.Command, args []string) error {
		return yasdiExporter.Run()
	},
}

var yasdiExporter = &YasdiExporter{}

func init() {
	rootCmd.PersistentFlags().StringVar(&yasdiExporter.yasdi.Device, "device", "/dev/ttyUSB0", "serial device to connect to")
	rootCmd.PersistentFlags().StringVar(&yasdiExporter.yasdi.Protocol, "protocol", "SunnyNet", "serial device to connect to")
	rootCmd.PersistentFlags().StringVar(&yasdiExporter.yasdi.Baudrate, "baudrate", "1200", "baudrate of the serial interface")
	rootCmd.PersistentFlags().StringVar(&yasdiExporter.yasdi.Media, "media", "RS485", "baudrate of the serial interface")
	rootCmd.PersistentFlags().StringVar(&yasdiExporter.metricsAddr, "metrics-addr", "0.0.0.0:9123", "address to bind to the metrics server")
	rootCmd.PersistentFlags().StringVar(&yasdiExporter.influxdbURL, "influxdb-url", "", "influxdb URL to export yield metrics to (default disabled)")
	rootCmd.PersistentFlags().StringSliceVar(&yasdiExporter.inverterSerials, "inverter-serial", []string{}, "serial number(s) of inverter(s) to watch")
}

type YasdiExporter struct {
	log *logrus.Entry

	metrics *metrics.Metrics
	stopCh  chan struct{}
	conn    *yasdi.Connection

	// arguments
	yasdi yasdi.ConnectionSettings

	inverterSerials []string
	influxdbURL     string
	metricsAddr     string
}

func (y *YasdiExporter) Run() error {
	var err error
	var wg sync.WaitGroup

	y.stopCh = make(chan struct{})
	logger := logrus.New()
	logger.Level = logrus.DebugLevel
	logger.Formatter = &logrus.JSONFormatter{}
	y.log = logrus.NewEntry(logger)

	// handle signals
	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-signalCh
		if y.stopCh != nil {
			y.log.Infof("received signal (%v), stopping", sig)
			close(y.stopCh)
		}
	}()

	// start metrics server
	y.metrics = metrics.New(y.log)
	wg.Add(1)
	go func() {
		defer wg.Done()
		y.metrics.Addr = y.metricsAddr
		y.metrics.Start(y.stopCh)
	}()

	// TODO: detect inverts or create and search for them

	// TODO: register all statuses

	// TODO: handle non monotonous yield

	// loop for inverter connection
	wg.Add(1)
	go func() {
		defer wg.Done()
		y.yasdiLoop()
	}()

	wg.Wait()

	return err

}

func (y *YasdiExporter) yasdiLoop() {
	firstRun := make(chan struct{}, 1)
	firstRun <- struct{}{}
	for {
		select {
		case <-y.stopCh:
			return
		case <-time.Tick(60 * time.Second):
			// TODO: this should be exponentially growing
			break
		case <-firstRun:
			break
		}
		if err := y.yasdiConnect(); err != nil {
			y.log.Error(err)
		}
	}
}

func (y *YasdiExporter) yasdiConnect() (err error) {
	y.conn, err = yasdi.NewConnection(y.log, &y.yasdi)
	if err != nil {
		return fmt.Errorf("error connecting to yasdi: %s", err)
	}

	err = y.conn.Initialize()
	if err != nil {
		return fmt.Errorf("error initialising connection to yasdi: %s", err)
	}
	defer y.conn.Shutdown()

	err = y.conn.DetectDevices(2, y.stopCh)
	if err != nil {
		return fmt.Errorf("error detecting 2 devices: %s", err)
	}

	influxClient, err := influxdb.NewInfluxDB(y.influxdbURL)
	if err != nil {
		return fmt.Errorf("error reporting to influxdb: %s", err)
	}

	firstRun := make(chan struct{}, 1)
	firstRun <- struct{}{}
	for {
		select {
		case <-y.stopCh:
			return nil
		case <-time.Tick(150 * time.Second):
			break
		case <-firstRun:
			break
		}
		y.yasdiMeasure(influxClient)
	}

	return nil
}

func (y *YasdiExporter) yasdiMeasure(influxClient *influxdb.InfluxDB) {
	var values []int64
	var serials []string
	for _, device := range y.conn.DeviceHandles {
		status, err := device.Status()
		if err != nil {
			device.Log().Errorf("unable to get status: %s", err)
		} else {
			device.Log().Infof("status: %s", status)
		}

		serial := fmt.Sprintf("%d", device.Serial)

		totalYield, err := device.TotalYield()
		if err != nil {
			device.Log().Errorf("unable to get total yield: %s", err)
			continue
		}
		values = append(values, totalYield)
		serials = append(serials, fmt.Sprintf("%d", device.Serial))
		device.Log().Infof("total yield: %d", totalYield)
	}
	if len(values) > 0 {
		err := influxClient.SendValues(serials, values)
		if err != nil {
			y.log.Errorf("unable to send data to influx: %s", err)
		}
	}
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		yasdiExporter.log.Fatal(err)
	}
}
