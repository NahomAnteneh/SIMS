package vsphere

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/vim25/soap"
	"github.com/vmware/govmomi/vim25/types"
)

var (
	ErrFakeDisconnect = errors.New("connection reset by peer")
	ErrFakeMalformed  = errors.New("malformed update payload")
)

// NewInvalidCollectorVersionFault constructs a typed vSphere InvalidCollectorVersion fault.
func NewInvalidCollectorVersionFault() error {
	return soap.WrapSoapFault(&soap.Fault{
		Code:   "ServerFaultCode",
		String: "InvalidCollectorVersion",
		Detail: struct {
			Fault types.AnyType `xml:",any,typeattr"`
		}{
			Fault: &types.InvalidCollectorVersion{},
		},
	})
}

// NewNotAuthenticatedFault constructs a typed vSphere NotAuthenticated fault.
func NewNotAuthenticatedFault() error {
	return soap.WrapSoapFault(&soap.Fault{
		Code:   "ServerFaultCode",
		String: "The session is not authenticated.",
		Detail: struct {
			Fault types.AnyType `xml:",any,typeattr"`
		}{
			Fault: &types.NotAuthenticated{},
		},
	})
}

// NewInvalidLoginFault constructs a typed vSphere InvalidLogin fault.
func NewInvalidLoginFault() error {
	return soap.WrapSoapFault(&soap.Fault{
		Code:   "ServerFaultCode",
		String: "Cannot complete login due to an incorrect user name or password.",
		Detail: struct {
			Fault types.AnyType `xml:",any,typeattr"`
		}{
			Fault: &types.InvalidLogin{},
		},
	})
}

// FakeSession implements Session for deterministic unit testing.
type FakeSession struct {
	mu sync.Mutex

	connectErr      error
	loginErr        error
	currentTimeErr  error
	currentTimeDelay time.Duration
	about           *types.AboutInfo
	connected       bool
	loggedIn        bool
	logoutCalled    bool
	reconnectCalled int
	currentTime     time.Time
}

func NewFakeSession() *FakeSession {
	return &FakeSession{
		about: &types.AboutInfo{
			FullName:   "VMware vCenter Server 8.0.3 build-24022515 (Fake)",
			Version:    "8.0.3",
			ApiVersion: "8.0.3.0",
		},
		currentTime: time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC),
	}
}

func (s *FakeSession) InjectConnectError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.connectErr = err
}

func (s *FakeSession) InjectLoginError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loginErr = err
}

func (s *FakeSession) InjectCurrentTimeError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.currentTimeErr = err
}

func (s *FakeSession) InjectCurrentTimeDelay(delay time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.currentTimeDelay = delay
}

func (s *FakeSession) SetAbout(about *types.AboutInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.about = about
}

func (s *FakeSession) Connect(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.connectErr != nil {
		return s.connectErr
	}
	s.connected = true
	return nil
}

func (s *FakeSession) Login(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loginErr != nil {
		return s.loginErr
	}
	s.loggedIn = true
	return nil
}

func (s *FakeSession) CurrentTime(ctx context.Context) (time.Time, error) {
	s.mu.Lock()
	err := s.currentTimeErr
	delay := s.currentTimeDelay
	t := s.currentTime
	s.mu.Unlock()

	if delay > 0 {
		select {
		case <-ctx.Done():
			return time.Time{}, ctx.Err()
		case <-time.After(delay):
		}
	}

	if err != nil {
		return time.Time{}, err
	}
	return t, nil
}

func (s *FakeSession) Logout(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logoutCalled = true
	s.loggedIn = false
	s.connected = false
	return nil
}

func (s *FakeSession) Reconnect(ctx context.Context) error {
	s.mu.Lock()
	s.reconnectCalled++
	s.mu.Unlock()
	if err := s.Connect(ctx); err != nil {
		return err
	}
	return s.Login(ctx)
}

func (s *FakeSession) About() *types.AboutInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.about
}

func (s *FakeSession) Client() *govmomi.Client {
	return nil
}

// FakePropertyFilter implements PropertyFilter.
type FakePropertyFilter struct {
	Ref       types.ManagedObjectReference
	Destroyed bool
}

func (f *FakePropertyFilter) Destroy(ctx context.Context) error {
	f.Destroyed = true
	return nil
}

func (f *FakePropertyFilter) Reference() types.ManagedObjectReference {
	return f.Ref
}

// FakePropertyCollector implements PropertyCollector with fault injection.
type FakePropertyCollector struct {
	mu sync.Mutex

	Ref types.ManagedObjectReference

	injectedFault   error
	waitDelay       time.Duration
	truncatedFlag   *bool
	updatesQueue    []*types.UpdateSet
	currentVersion  string
	createdFilters  []*FakePropertyFilter
	canceledWaits   int
	destroyedCount  int
}

func NewFakePropertyCollector() *FakePropertyCollector {
	return &FakePropertyCollector{
		Ref:            types.ManagedObjectReference{Type: "PropertyCollector", Value: "propertyCollector"},
		currentVersion: "1",
	}
}

func (f *FakePropertyCollector) InjectFault(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.injectedFault = err
}

func (f *FakePropertyCollector) InjectInvalidCollectorVersion() {
	f.InjectFault(NewInvalidCollectorVersionFault())
}

func (f *FakePropertyCollector) InjectHungWaitForUpdates(delay time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.waitDelay = delay
}

func (f *FakePropertyCollector) InjectTruncatedUpdates(truncated bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.truncatedFlag = types.NewBool(truncated)
}

func (f *FakePropertyCollector) InjectDisconnect() {
	f.InjectFault(ErrFakeDisconnect)
}

func (f *FakePropertyCollector) InjectAuthFailure() {
	f.InjectFault(NewNotAuthenticatedFault())
}

func (f *FakePropertyCollector) EnqueueUpdate(set *types.UpdateSet) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updatesQueue = append(f.updatesQueue, set)
}

func (f *FakePropertyCollector) Reference() types.ManagedObjectReference {
	return f.Ref
}

func (f *FakePropertyCollector) CreateFilter(ctx context.Context, spec types.PropertyFilterSpec) (PropertyFilter, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	filter := &FakePropertyFilter{
		Ref: types.ManagedObjectReference{
			Type:  "PropertyFilter",
			Value: fmt.Sprintf("session[%s]filter[%d]", f.Ref.Value, len(f.createdFilters)+1),
		},
	}
	f.createdFilters = append(f.createdFilters, filter)
	return filter, nil
}

func (f *FakePropertyCollector) WaitForUpdatesEx(ctx context.Context, version string, options *types.WaitOptions) (*types.UpdateSet, error) {
	f.mu.Lock()
	faultErr := f.injectedFault
	delay := f.waitDelay
	truncated := f.truncatedFlag
	var update *types.UpdateSet
	if len(f.updatesQueue) > 0 {
		update = f.updatesQueue[0]
		f.updatesQueue = f.updatesQueue[1:]
	}
	f.mu.Unlock()

	if delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}

	if faultErr != nil {
		return nil, faultErr
	}

	if update != nil {
		if truncated != nil {
			update.Truncated = truncated
		}
		return update, nil
	}

	// Default: return empty update with incremented version
	f.mu.Lock()
	f.currentVersion = fmt.Sprintf("%s_next", version)
	res := &types.UpdateSet{
		Version: f.currentVersion,
	}
	if truncated != nil {
		res.Truncated = truncated
	}
	f.mu.Unlock()
	return res, nil
}

func (f *FakePropertyCollector) CancelWaitForUpdates(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.canceledWaits++
	return nil
}

func (f *FakePropertyCollector) Destroy(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.destroyedCount++
	return nil
}

func (f *FakePropertyCollector) CreatedFilters() []*FakePropertyFilter {
	f.mu.Lock()
	defer f.mu.Unlock()
	res := make([]*FakePropertyFilter, len(f.createdFilters))
	copy(res, f.createdFilters)
	return res
}

// FakeEventCollector implements EventCollector with event queues and gap injection.
type FakeEventCollector struct {
	mu sync.Mutex

	Ref          types.ManagedObjectReference
	eventsQueue  []types.BaseEvent
	injectedErr  error
	readDelay    time.Duration
	resetCount   int
	destroyCount int
}

func NewFakeEventCollector() *FakeEventCollector {
	return &FakeEventCollector{
		Ref: types.ManagedObjectReference{Type: "EventHistoryCollector", Value: "session[fake]collector[1]"},
	}
}

func (f *FakeEventCollector) InjectFault(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.injectedErr = err
}

func (f *FakeEventCollector) InjectHungRead(delay time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.readDelay = delay
}

func (f *FakeEventCollector) EnqueueEvents(events ...types.BaseEvent) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.eventsQueue = append(f.eventsQueue, events...)
}

func (f *FakeEventCollector) LatestPage(ctx context.Context) ([]types.BaseEvent, error) {
	return f.ReadNextEvents(ctx, 100)
}

func (f *FakeEventCollector) ReadNextEvents(ctx context.Context, maxCount int32) ([]types.BaseEvent, error) {
	f.mu.Lock()
	err := f.injectedErr
	delay := f.readDelay
	s := int(maxCount)
	if s > len(f.eventsQueue) {
		s = len(f.eventsQueue)
	}
	batch := f.eventsQueue[:s]
	f.eventsQueue = f.eventsQueue[s:]
	f.mu.Unlock()

	if delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}

	if err != nil {
		return nil, err
	}
	return batch, nil
}

func (f *FakeEventCollector) Reset(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resetCount++
	return nil
}

func (f *FakeEventCollector) Destroy(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.destroyCount++
	return nil
}

func (f *FakeEventCollector) Reference() types.ManagedObjectReference {
	return f.Ref
}

// FakeEventManager implements EventManager returning a FakeEventCollector.
type FakeEventManager struct {
	mu sync.Mutex

	createdCollectors []*FakeEventCollector
	createErr         error
}

func NewFakeEventManager() *FakeEventManager {
	return &FakeEventManager{}
}

func (m *FakeEventManager) InjectCreateError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.createErr = err
}

func (m *FakeEventManager) CreateCollectorForEvents(ctx context.Context, filter types.EventFilterSpec) (EventCollector, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.createErr != nil {
		return nil, m.createErr
	}
	c := NewFakeEventCollector()
	m.createdCollectors = append(m.createdCollectors, c)
	return c, nil
}
