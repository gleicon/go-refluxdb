package mqtt

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"

	"github.com/gleicon/go-refluxdb/internal/persistence"
	"github.com/gleicon/go-refluxdb/internal/protocol"
	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/storage"
	"github.com/mochi-mqtt/server/v2/listeners"
	"github.com/mochi-mqtt/server/v2/packets"
	"github.com/mochi-mqtt/server/v2/system"
	"github.com/sirupsen/logrus"
)

// Server represents an MQTT server
type Server struct {
	addr      string
	db        *persistence.Manager
	server    *mqtt.Server
	wg        sync.WaitGroup
	mu        sync.Mutex
	isRunning bool
	log       *logrus.Logger
}

// New creates a new MQTT server
func New(addr string, db *persistence.Manager, logger *logrus.Logger) *Server {
	//logger := logrus.New()
	//logger.SetLevel(logrus.DebugLevel)
	return &Server{
		addr: addr,
		db:   db,
		log:  logger,
	}
}

// messageHook implements the mqtt.Hook interface for handling published messages
type messageHook struct {
	server *Server
}

func (h *messageHook) ID() string {
	return "message-handler"
}

func (h *messageHook) Provides(b byte) bool {
	return b == 'p' // OnPublished
}

func (h *messageHook) Init(config any) error {
	return nil
}

func (h *messageHook) OnPublish(cl *mqtt.Client, pk packets.Packet) (packets.Packet, error) {
	// Get the topic and payload
	topic := pk.TopicName
	payload := string(pk.Payload)

	h.server.log.Debugf("Received message on topic %s: %s", topic, payload)

	// Parse topic to get database and measurement
	parts := strings.Split(topic, "::")
	if len(parts) != 2 {
		h.server.log.Errorf("Invalid topic format: %s. Expected format: db::measurement", topic)
		return pk, nil
	}

	// Split payload into lines
	lines := strings.Split(strings.TrimSpace(payload), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Parse line protocol
		proto, err := protocol.Parse(line)
		if err != nil {
			h.server.log.Errorf("Error parsing line protocol: %v", err)
			continue
		}

		// Convert field values to float64
		for field, value := range proto.Fields {
			var floatValue float64

			// Handle different field value types
			if strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"") {
				// String value - store as 1.0 (presence)
				value = strings.Trim(value, "\"")
				floatValue = 1.0
			} else if strings.HasSuffix(value, "i") {
				// Integer value
				numStr := value[:len(value)-1]
				if intVal, err := strconv.ParseInt(numStr, 10, 64); err == nil {
					floatValue = float64(intVal)
				} else {
					h.server.log.Errorf("Invalid integer value: %s", value)
					continue
				}
			} else if strings.ToLower(value) == "true" {
				floatValue = 1.0
			} else if strings.ToLower(value) == "false" {
				floatValue = 0.0
			} else {
				// Try to parse as float
				if val, err := strconv.ParseFloat(value, 64); err == nil {
					floatValue = val
				} else {
					h.server.log.Errorf("Invalid numeric value: %s", value)
					continue
				}
			}

			// Save each field as a separate measurement
			err = h.server.db.SaveMeasurement(proto.Measurement, field, floatValue, proto.Tags, proto.Timestamp)
			if err != nil {
				h.server.log.Errorf("Error saving measurement: %v", err)
			}
		}
	}

	return pk, nil
}

func (h *messageHook) OnACLCheck(cl *mqtt.Client, topic string, write bool) bool {
	return true
}

func (h *messageHook) OnAuthPacket(cl *mqtt.Client, pk packets.Packet) (packets.Packet, error) {
	h.server.log.WithFields(logrus.Fields{
		"client_id": cl.ID,
		"username":  cl.Properties.Username,
		"clean":     cl.Properties.Clean,
	}).Debug("Client auth packet")
	return pk, nil
}

func (h *messageHook) OnClientExpired(cl *mqtt.Client) {
}

func (h *messageHook) OnConnect(cl *mqtt.Client, pk packets.Packet) error {
	h.server.log.WithFields(logrus.Fields{
		"client_id": cl.ID,
		"username":  cl.Properties.Username,
		"clean":     cl.Properties.Clean,
	}).Debug("Client connecting")
	return nil
}

func (h *messageHook) OnConnectAuthenticate(cl *mqtt.Client, pk packets.Packet) bool {
	h.server.log.WithFields(logrus.Fields{
		"client_id": cl.ID,
		"username":  cl.Properties.Username,
		"clean":     cl.Properties.Clean,
		"protocol":  cl.Properties.ProtocolVersion,
	}).Debug("Client authenticating - allowing connection without credentials")
	return true
}

func (h *messageHook) OnDisconnect(cl *mqtt.Client, err error, expire bool) {
}

func (h *messageHook) OnPacketEncode(cl *mqtt.Client, pk packets.Packet) packets.Packet {
	return pk
}

func (h *messageHook) OnPacketIDExhausted(cl *mqtt.Client, pk packets.Packet) {
}

func (h *messageHook) OnPacketProcessed(cl *mqtt.Client, pk packets.Packet, err error) {
}

func (h *messageHook) OnPacketRead(cl *mqtt.Client, pk packets.Packet) (packets.Packet, error) {
	return pk, nil
}

func (h *messageHook) OnPacketSent(cl *mqtt.Client, pk packets.Packet, b []byte) {
}

func (h *messageHook) OnPublishDropped(cl *mqtt.Client, pk packets.Packet) {
}

func (h *messageHook) OnPublished(cl *mqtt.Client, pk packets.Packet) {
}

func (h *messageHook) OnQosComplete(cl *mqtt.Client, pk packets.Packet) {
}

func (h *messageHook) OnQosDropped(cl *mqtt.Client, pk packets.Packet) {
}

func (h *messageHook) OnQosPublish(cl *mqtt.Client, pk packets.Packet, expiry int64, qos int) {
}

func (h *messageHook) OnRetainMessage(cl *mqtt.Client, pk packets.Packet, expiry int64) {
}

func (h *messageHook) OnRetainPublished(cl *mqtt.Client, pk packets.Packet) {
}

func (h *messageHook) OnRetainedExpired(topic string) {
}

func (h *messageHook) OnSelectSubscribers(subs *mqtt.Subscribers, pk packets.Packet) *mqtt.Subscribers {
	return subs
}

func (h *messageHook) OnSessionEstablish(cl *mqtt.Client, pk packets.Packet) {
}

func (h *messageHook) OnSessionEstablished(cl *mqtt.Client, pk packets.Packet) {
}

func (h *messageHook) OnStarted() {
}

func (h *messageHook) OnStopped() {
}

func (h *messageHook) OnSubscribe(cl *mqtt.Client, pk packets.Packet) packets.Packet {
	return pk
}

func (h *messageHook) OnSubscribed(cl *mqtt.Client, pk packets.Packet, reason []byte) {
}

func (h *messageHook) OnUnsubscribe(cl *mqtt.Client, pk packets.Packet) packets.Packet {
	return pk
}

func (h *messageHook) OnUnsubscribed(cl *mqtt.Client, pk packets.Packet) {
}

func (h *messageHook) OnWill(cl *mqtt.Client, will mqtt.Will) (mqtt.Will, error) {
	return will, nil
}

func (h *messageHook) OnWillSent(cl *mqtt.Client, pk packets.Packet) {
}

func (h *messageHook) OnSysInfoTick(sys *system.Info) {
}

func (h *messageHook) SetOpts(logger *slog.Logger, opts *mqtt.HookOptions) {
}

func (h *messageHook) Stop() error {
	return nil
}

func (h *messageHook) StoredClients() ([]storage.Client, error) {
	return nil, nil
}

func (h *messageHook) StoredInflightMessages() ([]storage.Message, error) {
	return nil, nil
}

func (h *messageHook) StoredRetainedMessages() ([]storage.Message, error) {
	return nil, nil
}

func (h *messageHook) StoredSubscriptions() ([]storage.Subscription, error) {
	return nil, nil
}

func (h *messageHook) StoredSysInfo() (storage.SystemInfo, error) {
	return storage.SystemInfo{}, nil
}

// aclHook implements the mqtt.Hook interface for ACL checks
type aclHook struct{}

func (h *aclHook) ID() string {
	return "acl-checker"
}

func (h *aclHook) Provides(b byte) bool {
	return b == 'a' // ACLCheck
}

func (h *aclHook) Init(config any) error {
	return nil
}

func (h *aclHook) OnPublish(cl *mqtt.Client, pk packets.Packet) (packets.Packet, error) {
	return pk, nil
}

func (h *aclHook) OnACLCheck(cl *mqtt.Client, topic string, write bool) bool {
	return true
}

func (h *aclHook) OnAuthPacket(cl *mqtt.Client, pk packets.Packet) (packets.Packet, error) {
	return pk, nil
}

func (h *aclHook) OnClientExpired(cl *mqtt.Client) {
}

func (h *aclHook) OnConnect(cl *mqtt.Client, pk packets.Packet) error {
	return nil
}

func (h *aclHook) OnConnectAuthenticate(cl *mqtt.Client, pk packets.Packet) bool {
	// Always allow connections without requiring credentials
	return true
}

func (h *aclHook) OnDisconnect(cl *mqtt.Client, err error, expire bool) {
}

func (h *aclHook) OnPacketEncode(cl *mqtt.Client, pk packets.Packet) packets.Packet {
	return pk
}

func (h *aclHook) OnPacketIDExhausted(cl *mqtt.Client, pk packets.Packet) {
}

func (h *aclHook) OnPacketProcessed(cl *mqtt.Client, pk packets.Packet, err error) {
}

func (h *aclHook) OnPacketRead(cl *mqtt.Client, pk packets.Packet) (packets.Packet, error) {
	return pk, nil
}

func (h *aclHook) OnPacketSent(cl *mqtt.Client, pk packets.Packet, b []byte) {
}

func (h *aclHook) OnPublishDropped(cl *mqtt.Client, pk packets.Packet) {
}

func (h *aclHook) OnPublished(cl *mqtt.Client, pk packets.Packet) {
}

func (h *aclHook) OnQosComplete(cl *mqtt.Client, pk packets.Packet) {
}

func (h *aclHook) OnQosDropped(cl *mqtt.Client, pk packets.Packet) {
}

func (h *aclHook) OnQosPublish(cl *mqtt.Client, pk packets.Packet, expiry int64, qos int) {
}

func (h *aclHook) OnRetainMessage(cl *mqtt.Client, pk packets.Packet, expiry int64) {
}

func (h *aclHook) OnRetainPublished(cl *mqtt.Client, pk packets.Packet) {
}

func (h *aclHook) OnRetainedExpired(topic string) {
}

func (h *aclHook) OnSelectSubscribers(subs *mqtt.Subscribers, pk packets.Packet) *mqtt.Subscribers {
	return subs
}

func (h *aclHook) OnSessionEstablish(cl *mqtt.Client, pk packets.Packet) {
}

func (h *aclHook) OnSessionEstablished(cl *mqtt.Client, pk packets.Packet) {
}

func (h *aclHook) OnStarted() {
}

func (h *aclHook) OnStopped() {
}

func (h *aclHook) OnSubscribe(cl *mqtt.Client, pk packets.Packet) packets.Packet {
	return pk
}

func (h *aclHook) OnSubscribed(cl *mqtt.Client, pk packets.Packet, reason []byte) {
}

func (h *aclHook) OnUnsubscribe(cl *mqtt.Client, pk packets.Packet) packets.Packet {
	return pk
}

func (h *aclHook) OnUnsubscribed(cl *mqtt.Client, pk packets.Packet) {
}

func (h *aclHook) OnWill(cl *mqtt.Client, will mqtt.Will) (mqtt.Will, error) {
	return will, nil
}

func (h *aclHook) OnWillSent(cl *mqtt.Client, pk packets.Packet) {
}

func (h *aclHook) OnSysInfoTick(sys *system.Info) {
}

func (h *aclHook) SetOpts(logger *slog.Logger, opts *mqtt.HookOptions) {
}

func (h *aclHook) Stop() error {
	return nil
}

func (h *aclHook) StoredClients() ([]storage.Client, error) {
	return nil, nil
}

func (h *aclHook) StoredInflightMessages() ([]storage.Message, error) {
	return nil, nil
}

func (h *aclHook) StoredRetainedMessages() ([]storage.Message, error) {
	return nil, nil
}

func (h *aclHook) StoredSubscriptions() ([]storage.Subscription, error) {
	return nil, nil
}

func (h *aclHook) StoredSysInfo() (storage.SystemInfo, error) {
	return storage.SystemInfo{}, nil
}

// Start starts the MQTT server
func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.isRunning {
		s.mu.Unlock()
		return fmt.Errorf("server is already running")
	}
	s.isRunning = true
	s.mu.Unlock()

	// Create new MQTT server
	s.server = mqtt.New(&mqtt.Options{
		InlineClient: true,
		Capabilities: &mqtt.Capabilities{
			MaximumMessageExpiryInterval: 0,
			MaximumClientWritesPending:   1000,
			MaximumSessionExpiryInterval: 0,
			ReceiveMaximum:               100,
			MaximumQos:                   2,
			RetainAvailable:              byte(1),
			MaximumPacketSize:            0,
			TopicAliasMaximum:            10,
			WildcardSubAvailable:         byte(1),
			SubIDAvailable:               byte(1),
			SharedSubAvailable:           byte(0),
		},
	})

	// Add hooks in the correct order
	s.server.AddHook(&aclHook{}, nil)              // Add ACL hook first
	s.server.AddHook(&messageHook{server: s}, nil) // Add message hook second

	// Add TCP listener
	tcp := listeners.NewTCP("tcp", s.addr, nil)
	err := s.server.AddListener(tcp)
	if err != nil {
		return fmt.Errorf("failed to add TCP listener: %v", err)
	}

	// Start the server
	err = s.server.Serve()
	if err != nil {
		return fmt.Errorf("failed to start MQTT server: %v", err)
	}

	s.log.Infof("Started MQTT server on %s", s.addr)

	// Handle shutdown
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		<-ctx.Done()
		s.Stop()
	}()

	return nil
}

// Stop stops the MQTT server
func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.isRunning {
		return nil
	}

	if s.server != nil {
		s.server.Close()
		s.server = nil
	}

	s.wg.Wait()
	s.isRunning = false
	return nil
}

// Addr returns the server's address
func (s *Server) Addr() string {
	return s.addr
}
