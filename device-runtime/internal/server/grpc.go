package server

import (
	"context"
	"runtime"
	"time"

	"github.com/rs/zerolog/log"
	simulatorv1 "github.com/virtual-iot-simulator/device-runtime/gen/go/simulator/v1"
	"github.com/virtual-iot-simulator/device-runtime/internal/device"
	"github.com/virtual-iot-simulator/device-runtime/internal/metrics"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/emptypb"
)

// RuntimeServer implements DeviceRuntimeServiceServer.
type RuntimeServer struct {
	simulatorv1.UnimplementedDeviceRuntimeServiceServer
	manager     *device.Manager
	broadcaster *Broadcaster
	startTime   time.Time
}

// NewRuntimeServer wires the Manager and Broadcaster into the gRPC service.
func NewRuntimeServer(mgr *device.Manager, bc *Broadcaster) *RuntimeServer {
	return &RuntimeServer{
		manager:     mgr,
		broadcaster: bc,
		startTime:   time.Now(),
	}
}

// SpawnDevices spawns one or more virtual devices.
func (s *RuntimeServer) SpawnDevices(_ context.Context, req *simulatorv1.SpawnDevicesRequest) (*simulatorv1.SpawnDevicesResponse, error) {
	if len(req.Specs) == 0 {
		return nil, status.Error(codes.InvalidArgument, "specs must not be empty")
	}

	spawned, failures := s.manager.Spawn(req.Specs)

	failedIDs := make([]string, 0, len(failures))
	for id := range failures {
		failedIDs = append(failedIDs, id)
	}

	return &simulatorv1.SpawnDevicesResponse{
		Spawned:         int32(spawned),
		FailedDeviceIds: failedIDs,
		FailureReasons:  failures,
	}, nil
}

// StopDevices stops devices matching the selector.
func (s *RuntimeServer) StopDevices(_ context.Context, req *simulatorv1.StopDevicesRequest) (*simulatorv1.StopDevicesResponse, error) {
	stopped, failures := s.manager.Stop(req.Selector, req.Graceful)

	failedIDs := make([]string, 0, len(failures))
	for id := range failures {
		failedIDs = append(failedIDs, id)
	}

	return &simulatorv1.StopDevicesResponse{
		Stopped:         int32(stopped),
		FailedDeviceIds: failedIDs,
	}, nil
}

// GetFleetStatus returns the current fleet status.
func (s *RuntimeServer) GetFleetStatus(_ context.Context, req *simulatorv1.GetFleetStatusRequest) (*simulatorv1.FleetStatus, error) {
	return s.manager.GetStatus(req.Selector), nil
}

// InjectFault injects a fault into matching devices.
func (s *RuntimeServer) InjectFault(_ context.Context, req *simulatorv1.InjectFaultRequest) (*emptypb.Empty, error) {
	if req.FaultType == simulatorv1.FaultType_FAULT_TYPE_UNSPECIFIED {
		return nil, status.Error(codes.InvalidArgument, "fault_type must be specified")
	}

	duration := time.Duration(0)
	if req.Duration != nil {
		duration = req.Duration.AsDuration()
	}

	params := map[string]any{}
	if req.Parameters != nil {
		params = req.Parameters.AsMap()
	}

	injected, failures := s.manager.InjectFault(req.Selector, req.FaultType, duration, params)
	metrics.FaultsInjectedTotal.WithLabelValues(req.FaultType.String()).Add(float64(injected))

	if len(failures) > 0 {
		log.Warn().Interface("failures", failures).Msg("InjectFault: some devices failed")
	}
	log.Info().
		Str("fault_type", req.FaultType.String()).
		Int("injected", injected).
		Msg("fault injected")

	return &emptypb.Empty{}, nil
}

// UpdateDeviceBehavior updates behavior params (generators) of matching devices.
func (s *RuntimeServer) UpdateDeviceBehavior(_ context.Context, req *simulatorv1.UpdateDeviceBehaviorRequest) (*emptypb.Empty, error) {
	updated, failures := s.manager.UpdateBehavior(req.Selector, req.BehaviorParams)

	if len(failures) > 0 {
		log.Warn().Interface("failures", failures).Msg("UpdateDeviceBehavior: some devices failed")
	}
	log.Info().Int("updated", updated).Msg("device behaviors updated")
	return &emptypb.Empty{}, nil
}

// StreamTelemetry subscribes to the telemetry broadcaster and sends batched points.
func (s *RuntimeServer) StreamTelemetry(req *simulatorv1.StreamTelemetryRequest, stream simulatorv1.DeviceRuntimeService_StreamTelemetryServer) error {
	batchSize := int(req.BatchSize)
	if batchSize <= 0 {
		batchSize = 100
	}

	flushInterval := 500 * time.Millisecond
	if req.FlushInterval != nil {
		flushInterval = req.FlushInterval.AsDuration()
	}

	sub := s.broadcaster.Subscribe()
	defer s.broadcaster.Unsubscribe(sub)

	var seq int64
	var batch []*simulatorv1.TelemetryPoint
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		seq++
		err := stream.Send(&simulatorv1.TelemetryBatch{
			Points:         batch,
			SequenceNumber: seq,
		})
		batch = batch[:0]
		return err
	}

	for {
		select {
		case <-stream.Context().Done():
			return stream.Context().Err()

		case pt, ok := <-sub:
			if !ok {
				return nil
			}
			if !matchesSelector(pt.DeviceId, req.Selector) {
				continue
			}
			batch = append(batch, pt)
			if len(batch) >= batchSize {
				if err := flush(); err != nil {
					return err
				}
			}

		case <-ticker.C:
			if err := flush(); err != nil {
				return err
			}
		}
	}
}

// StreamEvents streams device lifecycle events (spawn, stop, fault injection, errors).
func (s *RuntimeServer) StreamEvents(req *simulatorv1.StreamEventsRequest, stream simulatorv1.DeviceRuntimeService_StreamEventsServer) error {
	eventsCh := s.manager.EventsCh()
	for {
		select {
		case <-stream.Context().Done():
			return stream.Context().Err()

		case evt, ok := <-eventsCh:
			if !ok {
				return nil
			}
			if !matchesSelector(evt.DeviceId, req.Selector) {
				continue
			}
			if err := stream.Send(evt); err != nil {
				return err
			}
		}
	}
}

// GetRuntimeStatus returns a snapshot of runtime statistics.
func (s *RuntimeServer) GetRuntimeStatus(_ context.Context, _ *emptypb.Empty) (*simulatorv1.RuntimeStatus, error) {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	fleetStatus := s.manager.GetStatus(nil)
	uptime := time.Since(s.startTime)

	log.Debug().Int32("active_devices", fleetStatus.TotalDevices).Msg("runtime status polled")

	return &simulatorv1.RuntimeStatus{
		ActiveDevices:  fleetStatus.TotalDevices,
		GoroutineCount: int32(runtime.NumGoroutine()),
		// MessagesSentTotal is tracked via Prometheus; mirrored here for convenience.
		MessagesSentTotal:  0, // use /metrics for precise values
		MessagesPerSecond:  0,
		MemoryBytes:        int64(ms.Alloc),
		Uptime:             durationpb.New(uptime),
	}, nil
}

// matchesSelector returns true if deviceID is included in the selector (or selector is nil).
func matchesSelector(deviceID string, selector *simulatorv1.DeviceSelector) bool {
	if selector == nil {
		return true
	}
	switch s := selector.Selector.(type) {
	case *simulatorv1.DeviceSelector_DeviceIds:
		for _, id := range s.DeviceIds.Ids {
			if id == deviceID {
				return true
			}
		}
		return false
	case *simulatorv1.DeviceSelector_LabelSelector:
		// Label-based filtering on the stream side: pass all through;
		// the Manager's selector resolution handles label matching at spawn/stop.
		return true
	}
	return true
}
