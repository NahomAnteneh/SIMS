package vsphere

import (
	"context"
	"time"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/event"
	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/vim25/methods"
	"github.com/vmware/govmomi/vim25/types"
)

// Session abstracts vCenter session management for testing and fault injection.
type Session interface {
	Connect(ctx context.Context) error
	Login(ctx context.Context) error
	CurrentTime(ctx context.Context) (time.Time, error)
	Logout(ctx context.Context) error
	Reconnect(ctx context.Context) error
	About() *types.AboutInfo
	Client() *govmomi.Client
}

var _ Session = (*SessionManager)(nil)

// PropertyFilter models a created property filter instance.
type PropertyFilter interface {
	Destroy(ctx context.Context) error
	Reference() types.ManagedObjectReference
}

// PropertyCollector models the vSphere PropertyCollector operations used for streaming updates.
type PropertyCollector interface {
	CreateFilter(ctx context.Context, spec types.PropertyFilterSpec) (PropertyFilter, error)
	WaitForUpdatesEx(ctx context.Context, version string, options *types.WaitOptions) (*types.UpdateSet, error)
	CancelWaitForUpdates(ctx context.Context) error
	Destroy(ctx context.Context) error
	Reference() types.ManagedObjectReference
}

// EventCollector models an EventHistoryCollector session for reading and paging vSphere events.
type EventCollector interface {
	LatestPage(ctx context.Context) ([]types.BaseEvent, error)
	ReadNextEvents(ctx context.Context, maxCount int32) ([]types.BaseEvent, error)
	Reset(ctx context.Context) error
	Destroy(ctx context.Context) error
	Reference() types.ManagedObjectReference
}

// EventManager models the vSphere EventManager for creating event history collectors.
type EventManager interface {
	CreateCollectorForEvents(ctx context.Context, filter types.EventFilterSpec) (EventCollector, error)
}

// GovmomiPropertyFilter adapts a concrete *property.Filter to the PropertyFilter interface.
type GovmomiPropertyFilter struct {
	filter *property.Filter
}

func NewGovmomiPropertyFilter(f *property.Filter) *GovmomiPropertyFilter {
	return &GovmomiPropertyFilter{filter: f}
}

func (g *GovmomiPropertyFilter) Destroy(ctx context.Context) error {
	if g.filter == nil {
		return nil
	}
	return g.filter.Destroy(ctx)
}

func (g *GovmomiPropertyFilter) Reference() types.ManagedObjectReference {
	if g.filter == nil {
		return types.ManagedObjectReference{}
	}
	return g.filter.Reference()
}

// GovmomiPropertyCollector adapts a *govmomi.Client PropertyCollector to PropertyCollector.
type GovmomiPropertyCollector struct {
	client *govmomi.Client
	pc     *property.Collector
}

func NewGovmomiPropertyCollector(client *govmomi.Client) *GovmomiPropertyCollector {
	return &GovmomiPropertyCollector{
		client: client,
		pc:     property.DefaultCollector(client.Client),
	}
}

func (g *GovmomiPropertyCollector) Reference() types.ManagedObjectReference {
	return g.pc.Reference()
}

func (g *GovmomiPropertyCollector) CreateFilter(ctx context.Context, spec types.PropertyFilterSpec) (PropertyFilter, error) {
	req := types.CreateFilter{
		This: g.pc.Reference(),
		Spec: spec,
	}
	f, err := g.pc.CreateFilter(ctx, req)
	if err != nil {
		return nil, err
	}
	return NewGovmomiPropertyFilter(f), nil
}

func (g *GovmomiPropertyCollector) WaitForUpdatesEx(ctx context.Context, version string, options *types.WaitOptions) (*types.UpdateSet, error) {
	req := types.WaitForUpdatesEx{
		This:    g.pc.Reference(),
		Version: version,
		Options: options,
	}
	res, err := methods.WaitForUpdatesEx(ctx, g.client.Client, &req)
	if err != nil {
		return nil, err
	}
	if res == nil || res.Returnval == nil {
		return nil, nil
	}
	return res.Returnval, nil
}

func (g *GovmomiPropertyCollector) CancelWaitForUpdates(ctx context.Context) error {
	req := types.CancelWaitForUpdates{
		This: g.pc.Reference(),
	}
	_, err := methods.CancelWaitForUpdates(ctx, g.client.Client, &req)
	return err
}

func (g *GovmomiPropertyCollector) Destroy(ctx context.Context) error {
	return g.pc.Destroy(ctx)
}

// GovmomiEventCollector adapts a concrete *event.HistoryCollector to EventCollector.
type GovmomiEventCollector struct {
	collector *event.HistoryCollector
}

func NewGovmomiEventCollector(c *event.HistoryCollector) *GovmomiEventCollector {
	return &GovmomiEventCollector{collector: c}
}

func (g *GovmomiEventCollector) LatestPage(ctx context.Context) ([]types.BaseEvent, error) {
	return g.collector.LatestPage(ctx)
}

func (g *GovmomiEventCollector) ReadNextEvents(ctx context.Context, maxCount int32) ([]types.BaseEvent, error) {
	return g.collector.ReadNextEvents(ctx, maxCount)
}

func (g *GovmomiEventCollector) Reset(ctx context.Context) error {
	return g.collector.Reset(ctx)
}

func (g *GovmomiEventCollector) Destroy(ctx context.Context) error {
	return g.collector.Destroy(ctx)
}

func (g *GovmomiEventCollector) Reference() types.ManagedObjectReference {
	return g.collector.Reference()
}

// GovmomiEventManager adapts *govmomi.Client EventManager to EventManager.
type GovmomiEventManager struct {
	client *govmomi.Client
	em     *event.Manager
}

func NewGovmomiEventManager(client *govmomi.Client) *GovmomiEventManager {
	return &GovmomiEventManager{
		client: client,
		em:     event.NewManager(client.Client),
	}
}

func (g *GovmomiEventManager) CreateCollectorForEvents(ctx context.Context, filter types.EventFilterSpec) (EventCollector, error) {
	c, err := g.em.CreateCollectorForEvents(ctx, filter)
	if err != nil {
		return nil, err
	}
	return NewGovmomiEventCollector(c), nil
}
