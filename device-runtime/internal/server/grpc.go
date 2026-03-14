package server

import (
	"context"
	"runtime"
	"time"

	"github.com/rs/zerolog/log"
	simulatorv1 "github.com/virtual-iot-simulator/device-runtime/gen/go/simulator/v1"
	"github.com/virtual-iot-simulator/device-runtime/internal/device"
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
		Spawned:          int32(spawned),
		FailedDeviceIds:  failedIDs,
		FailureReasons:   failures,
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

// InjectFault injects a fault into matching devices (Phase 4 placeholder).
func (s *RuntimeServer) InjectFault(_ context.Context, _ *simulatorv1.InjectFaultRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, status.Error(codes.Unimplemented, "fault injection implemented in Phase 4")
}

// UpdateDeviceBehavior updates behavior params of matching devices (Phase 4 placeholder).
func (s *RuntimeServer) UpdateDeviceBehavior(_ context.Context, _ *simulatorv1.UpdateDeviceBehaviorRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, status.Error(codes.Unimplemented, "behavior update implemented in Phase 4")
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
			// Apply selector filter
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

// StreamEvents streams device lifecycle events (Phase 4 placeholder).
func (s *RuntimeServer) StreamEvents(_ *simulatorv1.StreamEventsRequest, stream simulatorv1.DeviceRuntimeService_StreamEventsServer) error {
	<-stream.Context().Done()
	return stream.Context().Err()
}

// GetRuntimeStatus returns a snapshot of runtime statistics.
func (s *RuntimeServer) GetRuntimeStatus(_ context.Context, _ *emptypb.Empty) (*simulatorv1.RuntimeStatus, error) {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	status := s.manager.GetStatus(nil)
	uptime := time.Since(s.startTime)

	log.Debug().Int32("active_devices", status.TotalDevices).Msg("runtime status polled")

	return &simulatorv1.RuntimeStatus{
		ActiveDevices:     status.TotalDevices,
		GoroutineCount:    int32(runtime.NumGoroutine()),
		MessagesSentTotal: 0, // Phase 5: wire metrics
		MemoryBytes:       int64(ms.Alloc),
		Uptime:            durationpb.New(uptime),
	}, nil
}

// matchesSelector returns true if deviceID is included in the selector (or selector is nil/empty).
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
		// Label-based filtering on the stream side is handled in device manager;
		// here we pass all (label info not available per-point). Phase 5 can refine.
		return true
	}
	return true
}
