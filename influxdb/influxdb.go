package influxdb

import (
	"fmt"
	"net/url"
	"path"
	"strings"

	influxdb "github.com/influxdata/influxdb/client/v2"
)

type InfluxDB struct {
	InfluxDBUsername    string
	InfluxDBPassword    string
	InfluxDBName        string
	InfluxDBURL         string
	InfluxDBMeasurement string

	InfluxDBBatchSize int
}

func NewInfluxDB(o string) (output *InfluxDB, err error) {
	url, err := url.Parse(o)
	if err != nil {
		return nil, fmt.Errorf("error parsing output url '%s': %s", o, err)
	}

	if strings.ToLower(url.Scheme) == "influxdb" {
		url.Scheme = "http"
	} else if strings.ToLower(url.Scheme) == "influxdbs" {
		url.Scheme = "https"
	} else {
		return nil, fmt.Errorf("unsupported url scheme '%s': %s", url.Scheme, err)
	}

	database := path.Base(url.Path)
	url.Path = path.Dir(url.Path)
	if url.Path == "/" {
		url.Path = ""
	}

	user := url.User.Username()
	password, ok := url.User.Password()
	if !ok {
		return nil, fmt.Errorf("error getting password '%s': %s", o, err)
	}

	url.User = nil

	output = &InfluxDB{
		InfluxDBUsername:    user,
		InfluxDBPassword:    password,
		InfluxDBURL:         url.String(),
		InfluxDBName:        database,
		InfluxDBBatchSize:   200,
		InfluxDBMeasurement: "inverters",
	}
	return output, nil
}

func (o *InfluxDB) initClient() (influxdb.Client, error) {
	c, err := influxdb.NewHTTPClient(influxdb.HTTPConfig{
		Addr:     o.InfluxDBURL,
		Username: o.InfluxDBUsername,
		Password: o.InfluxDBPassword,
	})
	return c, err
}

func (o *InfluxDB) SendValues(serials []string, values []int64) error {
	// Get client
	c, err := o.initClient()
	if err != nil {
		return fmt.Errorf("error initializing influxdb client: %s", err)
	}

	// Create a new point batch
	bp, err := influxdb.NewBatchPoints(influxdb.BatchPointsConfig{
		Database:  o.InfluxDBName,
		Precision: "s",
	})
	if err != nil {
		return fmt.Errorf("error creating batch point batch: %s", err)
	}

	// Create a point each and add to batch
	for pos := range values {
		tags := map[string]string{
			"serial": serials[pos],
		}

		fields := map[string]interface{}{
			"yield": float64(values[pos]),
		}

		pt, err := influxdb.NewPoint("inverters", tags, fields)
		if err != nil {
			return fmt.Errorf("error creating point: %s", err)
		}
		bp.AddPoint(pt)
	}

	return c.Write(bp)
}
