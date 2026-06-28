package service

import (
	"context"
	"errors"
	"strings"
	"syscall"
	"time"

	"github.com/Kentaro1043/dmonitor-improved/internal/config"
	"github.com/Kentaro1043/dmonitor-improved/internal/device"
	"github.com/Kentaro1043/dmonitor-improved/internal/repeater"
	"github.com/Kentaro1043/dmonitor-improved/internal/runtime"
)

type Service struct {
	manager *runtime.Manager
}

type RuntimeStatus struct {
	runtime.Snapshot
	Repeaters []repeater.Repeater `json:"repeaters"`
	Active    []repeater.Repeater `json:"activeRepeaters"`
}

type Status struct {
	Runtime  RuntimeStatus `json:"runtime"`
	Device   device.Status `json:"device"`
	UdevHint string        `json:"udevHint"`
	Config   config.Config `json:"config"`
}

func New(manager *runtime.Manager) *Service {
	return &Service{manager: manager}
}

func (s *Service) RootFS() string {
	return s.manager.RootFS()
}

func (s *Service) Status() Status {
	cfg, _ := config.Load(s.manager.RootFS())
	return Status{
		Runtime: RuntimeStatus{
			Snapshot:  s.manager.Snapshot(),
			Repeaters: repeater.ParseRepeaters(s.manager.RootFS()),
			Active:    repeater.ParseActiveRepeaters(s.manager.RootFS()),
		},
		Device:   device.Detect(),
		UdevHint: device.UdevHint(),
		Config:   cfg,
	}
}

func (s *Service) Config() (config.Config, error) {
	return config.Load(s.manager.RootFS())
}

func (s *Service) SaveConfig(cfg config.Config) (config.Config, error) {
	return config.Save(s.manager.RootFS(), cfg)
}

func (s *Service) StartRPTConn(ctx context.Context) (Status, error) {
	if err := s.manager.StartRPTConn(ctx); err != nil {
		return Status{}, err
	}
	return s.Status(), nil
}

func (s *Service) StopRPTConn(ctx context.Context) (Status, error) {
	if err := s.manager.StopRPTConn(ctx); err != nil {
		return Status{}, err
	}
	return s.Status(), nil
}

func (s *Service) Connect(ctx context.Context, req runtime.Connection) (Status, error) {
	if strings.TrimSpace(req.Address) == "" || strings.TrimSpace(req.AreaCallsign) == "" {
		return Status{}, errors.New("address and areaCallsign are required")
	}
	req.ConnectCallsign = strings.TrimSpace(req.ConnectCallsign)
	req.Address = strings.TrimSpace(req.Address)
	req.Port = strings.TrimSpace(req.Port)
	req.AreaCallsign = strings.ToUpper(strings.TrimSpace(req.AreaCallsign))
	req.ZoneCallsign = strings.ToUpper(strings.TrimSpace(req.ZoneCallsign))
	if err := s.manager.Connect(ctx, req); err != nil {
		return Status{}, err
	}
	return s.Status(), nil
}

func (s *Service) Disconnect(ctx context.Context) (Status, error) {
	if err := s.manager.Disconnect(ctx); err != nil {
		return Status{}, err
	}
	return s.Status(), nil
}

func (s *Service) StartScan(ctx context.Context) (Status, error) {
	if err := s.manager.StartScan(ctx); err != nil {
		return Status{}, err
	}
	return s.Status(), nil
}

func (s *Service) StopScan(ctx context.Context) (Status, error) {
	if err := s.manager.StopScan(ctx); err != nil {
		return Status{}, err
	}
	return s.Status(), nil
}

func (s *Service) UpdateRepeaters(ctx context.Context) (Status, error) {
	if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 30*time.Second {
		var cancel func()
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}
	if err := s.manager.UpdateRepeaters(ctx); err != nil {
		return Status{}, err
	}
	return s.Status(), nil
}

func (s *Service) IncreaseBuffer() (Status, error) {
	if err := s.manager.SignalDMonitor(syscall.SIGUSR1); err != nil {
		return Status{}, err
	}
	return s.Status(), nil
}

func (s *Service) DecreaseBuffer() (Status, error) {
	if err := s.manager.SignalDMonitor(syscall.SIGUSR2); err != nil {
		return Status{}, err
	}
	return s.Status(), nil
}

func (s *Service) Logs() []runtime.LogEntry {
	return s.manager.Logs()
}
