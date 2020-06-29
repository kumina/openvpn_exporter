package exporters

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	"io"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type OpenvpnServerHeader struct {
	LabelColumns []string
	Metrics      []OpenvpnServerHeaderField
}

type OpenvpnServerHeaderField struct {
	Column    string
	Desc      *prometheus.Desc
	ValueType prometheus.ValueType
}

type OpenVPNExporter struct {
	statusPaths                 []string
	statusType					string
	openvpnUpDesc               *prometheus.Desc
	openvpnStatusUpdateTimeDesc *prometheus.Desc
	openvpnStatusApiBuildInfo	*prometheus.Desc
	openvpnConnectedClientsDesc *prometheus.Desc
	openvpnClientDescs          map[string]*prometheus.Desc
	openvpnServerHeaders        map[string]OpenvpnServerHeader
}

func NewOpenVPNExporter(statusPaths []string, ignoreIndividuals bool, statusType string) (*OpenVPNExporter, error) {
	// Metrics exported both for client and server statistics.
	openvpnUpDesc := prometheus.NewDesc(
		prometheus.BuildFQName("openvpn", "", "up"),
		"Whether scraping OpenVPN's metrics was successful.",
		[]string{"status_path"}, nil)

	updateLabels := []string{}

	if statusType == "api" {
		updateLabels = []string{"status_path", "instance_id"}
	} else {
		updateLabels = []string{"status_path"}
	}
	openvpnStatusUpdateTimeDesc := prometheus.NewDesc(
		prometheus.BuildFQName("openvpn", "", "status_update_time_seconds"),
		"UNIX timestamp at which the OpenVPN statistics were updated.",
		updateLabels, nil)

	openvpnStatusApiBuildInfo := prometheus.NewDesc(
		prometheus.BuildFQName("openvpn", "", "server_build_info"),
		"application and build information on the running OpenVPN system.",
		[]string{"status_path", "instance_id", "title"}, nil)

	// Metrics specific to OpenVPN servers.
	openvpnConnectedClientsDesc := prometheus.NewDesc(
		prometheus.BuildFQName("openvpn", "", "server_connected_clients"),
		"Number Of Connected Clients",
		[]string{"status_path"}, nil)

	// Metrics specific to OpenVPN clients.
	openvpnClientDescs := map[string]*prometheus.Desc{
		"TUN/TAP read bytes": prometheus.NewDesc(
			prometheus.BuildFQName("openvpn", "client", "tun_tap_read_bytes_total"),
			"Total amount of TUN/TAP traffic read, in bytes.",
			[]string{"status_path"}, nil),
		"TUN/TAP write bytes": prometheus.NewDesc(
			prometheus.BuildFQName("openvpn", "client", "tun_tap_write_bytes_total"),
			"Total amount of TUN/TAP traffic written, in bytes.",
			[]string{"status_path"}, nil),
		"TCP/UDP read bytes": prometheus.NewDesc(
			prometheus.BuildFQName("openvpn", "client", "tcp_udp_read_bytes_total"),
			"Total amount of TCP/UDP traffic read, in bytes.",
			[]string{"status_path"}, nil),
		"TCP/UDP write bytes": prometheus.NewDesc(
			prometheus.BuildFQName("openvpn", "client", "tcp_udp_write_bytes_total"),
			"Total amount of TCP/UDP traffic written, in bytes.",
			[]string{"status_path"}, nil),
		"Auth read bytes": prometheus.NewDesc(
			prometheus.BuildFQName("openvpn", "client", "auth_read_bytes_total"),
			"Total amount of authentication traffic read, in bytes.",
			[]string{"status_path"}, nil),
		"pre-compress bytes": prometheus.NewDesc(
			prometheus.BuildFQName("openvpn", "client", "pre_compress_bytes_total"),
			"Total amount of data before compression, in bytes.",
			[]string{"status_path"}, nil),
		"post-compress bytes": prometheus.NewDesc(
			prometheus.BuildFQName("openvpn", "client", "post_compress_bytes_total"),
			"Total amount of data after compression, in bytes.",
			[]string{"status_path"}, nil),
		"pre-decompress bytes": prometheus.NewDesc(
			prometheus.BuildFQName("openvpn", "client", "pre_decompress_bytes_total"),
			"Total amount of data before decompression, in bytes.",
			[]string{"status_path"}, nil),
		"post-decompress bytes": prometheus.NewDesc(
			prometheus.BuildFQName("openvpn", "client", "post_decompress_bytes_total"),
			"Total amount of data after decompression, in bytes.",
			[]string{"status_path"}, nil),
	}

	var serverHeaderClientLabels []string
	var serverHeaderClientLabelColumns []string
	var serverHeaderRoutingLabels []string
	var serverHeaderRoutingLabelColumns []string
	if ignoreIndividuals {
		serverHeaderClientLabels = []string{"status_path", "common_name"}
		serverHeaderClientLabelColumns = []string{"Common Name"}
		serverHeaderRoutingLabels = []string{"status_path", "common_name"}
		serverHeaderRoutingLabelColumns = []string{"Common Name"}
	} else {
		serverHeaderClientLabels = []string{"status_path", "common_name", "connection_time", "real_address", "virtual_address", "username"}
		serverHeaderClientLabelColumns = []string{"Common Name", "Connected Since (time_t)", "Real Address", "Virtual Address", "Username"}
		serverHeaderRoutingLabels = []string{"status_path", "common_name", "real_address", "virtual_address"}
		serverHeaderRoutingLabelColumns = []string{"Common Name", "Real Address", "Virtual Address"}
	}

	openvpnServerHeaders := map[string]OpenvpnServerHeader{
		"CLIENT_LIST": {
			LabelColumns: serverHeaderClientLabelColumns,
			Metrics: []OpenvpnServerHeaderField{
				{
					Column: "Bytes Received",
					Desc: prometheus.NewDesc(
						prometheus.BuildFQName("openvpn", "server", "client_received_bytes_total"),
						"Amount of data received over a connection on the VPN server, in bytes.",
						serverHeaderClientLabels, nil),
					ValueType: prometheus.CounterValue,
				},
				{
					Column: "Bytes Sent",
					Desc: prometheus.NewDesc(
						prometheus.BuildFQName("openvpn", "server", "client_sent_bytes_total"),
						"Amount of data sent over a connection on the VPN server, in bytes.",
						serverHeaderClientLabels, nil),
					ValueType: prometheus.CounterValue,
				},
			},
		},
		"ROUTING_TABLE": {
			LabelColumns: serverHeaderRoutingLabelColumns,
			Metrics: []OpenvpnServerHeaderField{
				{
					Column: "Last Ref (time_t)",
					Desc: prometheus.NewDesc(
						prometheus.BuildFQName("openvpn", "server", "route_last_reference_time_seconds"),
						"Time at which a route was last referenced, in seconds.",
						serverHeaderRoutingLabels, nil),
					ValueType: prometheus.GaugeValue,
				},
			},
		},
	}

	return &OpenVPNExporter{
		statusPaths:                 statusPaths,
		statusType:                  statusType,
		openvpnUpDesc:               openvpnUpDesc,
		openvpnStatusUpdateTimeDesc: openvpnStatusUpdateTimeDesc,
		openvpnStatusApiBuildInfo: 	 openvpnStatusApiBuildInfo,
		openvpnConnectedClientsDesc: openvpnConnectedClientsDesc,
		openvpnClientDescs:          openvpnClientDescs,
		openvpnServerHeaders:        openvpnServerHeaders,
	}, nil
}

// Converts OpenVPN status information into Prometheus metrics. This
// function automatically detects whether the file contains server or
// client metrics. For server metrics, it also distinguishes between the
// version 2 and 3 file formats.
func (e *OpenVPNExporter) collectStatusFromReader(statusPath string, file io.Reader, ch chan<- prometheus.Metric) error {
	reader := bufio.NewReader(file)
	buf, _ := reader.Peek(18)
	if bytes.HasPrefix(buf, []byte("TITLE,")) {
		// Server statistics, using format version 2.
		return e.collectServerStatusFromReader(statusPath, reader, ch, ",")
	} else if bytes.HasPrefix(buf, []byte("TITLE\t")) {
		// Server statistics, using format version 3. The only
		// difference compared to version 2 is that it uses tabs
		// instead of spaces.
		return e.collectServerStatusFromReader(statusPath, reader, ch, "\t")
	} else if bytes.HasPrefix(buf, []byte("OpenVPN STATISTICS")) {
		// Client statistics.
		return e.collectClientStatusFromReader(statusPath, reader, ch)
	} else {
		return fmt.Errorf("unexpected file contents: %q", buf)
	}
}

// Converts OpenVPN server status information into Prometheus metrics.
func (e *OpenVPNExporter) collectServerStatusFromReader(statusPath string, file io.Reader, ch chan<- prometheus.Metric, separator string) error {
	scanner := bufio.NewScanner(file)
	scanner.Split(bufio.ScanLines)
	headersFound := map[string][]string{}
	// counter of connected client
	numberConnectedClient := 0

	recordedMetrics := map[OpenvpnServerHeaderField][]string{}

	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), separator)
		if fields[0] == "END" && len(fields) == 1 {
			// Stats footer.
		} else if fields[0] == "GLOBAL_STATS" {
			// Global server statistics.
		} else if fields[0] == "HEADER" && len(fields) > 2 {
			// Column names for CLIENT_LIST and ROUTING_TABLE.
			headersFound[fields[1]] = fields[2:]
		} else if fields[0] == "TIME" && len(fields) == 3 {
			// Time at which the statistics were updated.
			timeStartStats, err := strconv.ParseFloat(fields[2], 64)
			if err != nil {
				return err
			}
			ch <- prometheus.MustNewConstMetric(
				e.openvpnStatusUpdateTimeDesc,
				prometheus.GaugeValue,
				timeStartStats,
				statusPath)
		} else if fields[0] == "TITLE" && len(fields) == 2 {
			// OpenVPN version number.
		} else if header, ok := e.openvpnServerHeaders[fields[0]]; ok {
			if fields[0] == "CLIENT_LIST" {
				numberConnectedClient++
			}
			// Entry that depends on a preceding HEADERS directive.
			columnNames, ok := headersFound[fields[0]]
			if !ok {
				return fmt.Errorf("%s should be preceded by HEADERS", fields[0])
			}
			if len(fields) != len(columnNames)+1 {
				return fmt.Errorf("HEADER for %s describes a different number of columns", fields[0])
			}

			// Store entry values in a map indexed by column name.
			columnValues := map[string]string{}
			for _, column := range header.LabelColumns {
				columnValues[column] = ""
			}
			for i, column := range columnNames {
				columnValues[column] = fields[i+1]
			}

			// Extract columns that should act as entry labels.
			labels := []string{statusPath}
			for _, column := range header.LabelColumns {
				labels = append(labels, columnValues[column])
			}

			// Export relevant columns as individual metrics.
			for _, metric := range header.Metrics {
				if columnValue, ok := columnValues[metric.Column]; ok {
					if l, _ := recordedMetrics[metric]; ! subslice(labels, l) {
						value, err := strconv.ParseFloat(columnValue, 64)
						if err != nil {
							return err
						}
						ch <- prometheus.MustNewConstMetric(
							metric.Desc,
							metric.ValueType,
							value,
							labels...)
						recordedMetrics[metric] = append(recordedMetrics[metric], labels...)
					} else {
						log.Printf("Metric entry with same labels: %s, %s", metric.Column, labels)
					}
				}
			}
		} else {
			return fmt.Errorf("unsupported key: %q", fields[0])
		}
	}
	// add the number of connected client
	ch <- prometheus.MustNewConstMetric(
		e.openvpnConnectedClientsDesc,
		prometheus.GaugeValue,
		float64(numberConnectedClient),
		statusPath)
	return scanner.Err()
}

// Does slice contain string
func contains(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}

// Is a sub-slice of slice
func subslice(sub []string, main []string) bool {
	if len(sub) > len(main) {return false}
	for _, s := range sub {
		if ! contains(main, s) {
			return false
		}
	}
	return true
}

// Converts OpenVPN client status information into Prometheus metrics.
func (e *OpenVPNExporter) collectClientStatusFromReader(statusPath string, file io.Reader, ch chan<- prometheus.Metric) error {
	scanner := bufio.NewScanner(file)
	scanner.Split(bufio.ScanLines)
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), ",")
		if fields[0] == "END" && len(fields) == 1 {
			// Stats footer.
		} else if fields[0] == "OpenVPN STATISTICS" && len(fields) == 1 {
			// Stats header.
		} else if fields[0] == "Updated" && len(fields) == 2 {
			// Time at which the statistics were updated.
			location, _ := time.LoadLocation("Local")
			timeParser, err := time.ParseInLocation("Mon Jan 2 15:04:05 2006", fields[1], location)
			if err != nil {
				return err
			}
			ch <- prometheus.MustNewConstMetric(
				e.openvpnStatusUpdateTimeDesc,
				prometheus.GaugeValue,
				float64(timeParser.Unix()),
				statusPath)
		} else if desc, ok := e.openvpnClientDescs[fields[0]]; ok && len(fields) == 2 {
			// Traffic counters.
			value, err := strconv.ParseFloat(fields[1], 64)
			if err != nil {
				return err
			}
			ch <- prometheus.MustNewConstMetric(
				desc,
				prometheus.CounterValue,
				value,
				statusPath)
		} else {
			return fmt.Errorf("unsupported key: %q", fields[0])
		}
	}
	return scanner.Err()
}

func (e *OpenVPNExporter) collectStatusFromApiJson(statusPath string, output []byte, ch chan<- prometheus.Metric) error {
	var outputInterface map[string]interface{}

	err := json.Unmarshal(output, &outputInterface)
	if err != nil {
		return err
	}

	return e.collectStatusFromApiInterface(statusPath, outputInterface, ch)
}

func (e *OpenVPNExporter) collectStatusFromApiInterface(statusPath string, outputInterface map[string]interface{}, ch chan<- prometheus.Metric) error {

	connectedClientCount := 0

	for instanceId, data := range outputInterface {
		if dataInterface, ok := data.(map[string]interface{}); ok {
			for category, categoryData := range dataInterface {
				if category == "title" {
					// Build information
					buildInfo := categoryData.(string)
					labels := []string{statusPath, instanceId, buildInfo}
					ch <- prometheus.MustNewConstMetric(
						e.openvpnStatusApiBuildInfo,
						prometheus.GaugeValue,
						1.0,
						labels...)
				} else if category == "time" {
					labels := []string{statusPath, instanceId}

					// Time at which the statistics were updated.
					time := categoryData.([]interface{})
					timeStartStats, err := strconv.ParseFloat(time[1].(string), 64)
					if err != nil {
						return err
					}
					ch <- prometheus.MustNewConstMetric(
						e.openvpnStatusUpdateTimeDesc,
						prometheus.GaugeValue,
						timeStartStats,
						labels...)
				} else if category == "client_list" || category == "routing_table" {
					for _, item := range categoryData.([]interface{}) {
						item := item.([]interface{})
						if header, ok := e.openvpnServerHeaders[strings.ToUpper(category)]; ok {
							if category == "client_list" {
								// increment client counter
								connectedClientCount ++
							}
							// Store entry values in a map indexed by column name.
							columnValues := map[string]string{}
							for _, column := range header.LabelColumns {
								var index int

								if category == "client_list" {
									switch {
									case column == "Common Name":
										index = 0
									case column == "Real Address":
										index = 1
									case column == "Virtual Address":
										index = 2
									case column == "Connected Since (time_t)":
										index = 7
									case column == "Username":
										index = 8
									}
								} else if category == "routing_table" {
									switch {
									case column == "Virtual Address":
										index = 0
									case column == "Common Name":
										index = 1
									case column == "Real Address":
										index = 2
									}
								}
								columnValues[column] = item[index].(string)
							}

							for _, metric := range header.Metrics {
								var index int

								switch {
								case metric.Column == "Bytes Received":
									index = 4
								case metric.Column == "Bytes Sent":
									index = 5
								case metric.Column == "Last Ref (time_t)":
									index = 4
								}
								columnValues[metric.Column] = item[index].(string)
							}

							// Extract columns that should act as entry labels.
							labels := []string{statusPath}
							for _, column := range header.LabelColumns {
								labels = append(labels, columnValues[column])
							}

							// Export relevant columns as individual metrics.
							for _, metric := range header.Metrics {
								if columnValue, ok := columnValues[metric.Column]; ok {
									value, err := strconv.ParseFloat(columnValue, 64)
									if err != nil {
										return err
									}
									ch <- prometheus.MustNewConstMetric(
										metric.Desc,
										metric.ValueType,
										value,
										labels...)
								}
							}
						}
					}
				}
			}
		}
	}

	// add the count of connected clients
	ch <- prometheus.MustNewConstMetric(
		e.openvpnConnectedClientsDesc,
		prometheus.GaugeValue,
		float64(connectedClientCount),
		statusPath)

	return nil
}

func (e *OpenVPNExporter) collectStatusFromFile(statusPath string, ch chan<- prometheus.Metric) error {
	conn, err := os.Open(statusPath)
	defer conn.Close()
	if err != nil {
		return err
	}
	return e.collectStatusFromReader(statusPath, conn, ch)
}

func (e *OpenVPNExporter) collectStatusFromApi(statusPath string, ch chan<- prometheus.Metric) error {
	cmd := exec.Command("/usr/local/openvpn_as/scripts/sacli", "VPNStatus")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		log.Fatalf("cmd.Run() failed with %s\n", err)
		return err
	}

	return e.collectStatusFromApiJson(statusPath, stdout.Bytes(), ch)
}

func (e *OpenVPNExporter) Describe(ch chan<- *prometheus.Desc) {
	ch <- e.openvpnUpDesc
}


func (e *OpenVPNExporter) Collect(ch chan<- prometheus.Metric) {
	// api mode
	if e.statusType == "api" {
		err := e.collectStatusFromApi(e.statusType, ch)
		if err == nil {
			if err == nil {
				ch <- prometheus.MustNewConstMetric(
					e.openvpnUpDesc,
					prometheus.GaugeValue,
					1.0,
					e.statusType)
			} else {
				log.Printf("Failed to scrape api: %s", err)
				ch <- prometheus.MustNewConstMetric(
					e.openvpnUpDesc,
					prometheus.GaugeValue,
					0.0,
					e.statusType)
			}
		}
		// File mode
	} else {
		for _, statusPath := range e.statusPaths {
			err := e.collectStatusFromFile(statusPath, ch)
			if err == nil {
				ch <- prometheus.MustNewConstMetric(
					e.openvpnUpDesc,
					prometheus.GaugeValue,
					1.0,
					statusPath)
			} else {
				log.Printf("Failed to scrape showq socket: %s", err)
				ch <- prometheus.MustNewConstMetric(
					e.openvpnUpDesc,
					prometheus.GaugeValue,
					0.0,
					statusPath)
			}
		}
	}
}
