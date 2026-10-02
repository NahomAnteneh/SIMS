package collector

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/infrastructure/sims/internal/domain"
	"github.com/infrastructure/sims/internal/telemetry"
	"github.com/vmware/govmomi/vapi/rest"
	"github.com/vmware/govmomi/vapi/tags"
	"github.com/vmware/govmomi/vim25"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

var (
	// ErrTaggingUnavailable is returned when the vSphere Automation REST API
	// is unreachable, unauthenticated, or unsupported (such as on vcsim).
	ErrTaggingUnavailable = errors.New("vsphere tagging rest service unavailable")
)

// TagFetcherConfig configures timeouts and batch limits for vSphere REST tagging operations.
type TagFetcherConfig struct {
	Timeout   time.Duration // Default: 15s
	BatchSize int           // Default: 500
}

// TagFetcher abstracts retrieving tags attached to vSphere managed objects.
type TagFetcher interface {
	FetchTagsForObjects(ctx context.Context, refs []types.ManagedObjectReference) (map[string][]domain.TagRecord, error)
	Close(ctx context.Context) error
}

// RestTagFetcher implements TagFetcher via the vSphere Automation REST API (vapi/rest, vapi/tags).
type RestTagFetcher struct {
	client    *rest.Client
	manager   *tags.Manager
	vcenterID string
	metrics   *telemetry.Metrics
	cfg       TagFetcherConfig

	mu          sync.RWMutex
	categories  map[string]string           // categoryID -> categoryName
	tagDefs     map[string]domain.TagRecord // tagID -> domain.TagRecord
	initialized bool
	isAvailable bool
}

// NewRestTagFetcher creates a new RestTagFetcher using the underlying vim25 SOAP connection.
func NewRestTagFetcher(
	c *vim25.Client,
	vcenterID string,
	cfg TagFetcherConfig,
	metrics *telemetry.Metrics,
) *RestTagFetcher {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 15 * time.Second
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 500
	}

	restClient := rest.NewClient(c)
	tagMgr := tags.NewManager(restClient)

	return &RestTagFetcher{
		client:      restClient,
		manager:     tagMgr,
		vcenterID:   vcenterID,
		metrics:     metrics,
		cfg:         cfg,
		categories:  make(map[string]string),
		tagDefs:     make(map[string]domain.TagRecord),
		isAvailable: true,
	}
}

// Init establishes the vSphere Automation REST session and preloads tag metadata.
func (f *RestTagFetcher) Init(ctx context.Context, user *url.Userinfo) error {
	ctx, cancel := context.WithTimeout(ctx, f.cfg.Timeout)
	defer cancel()

	var err error
	if user != nil {
		err = f.client.Login(ctx, user)
	} else {
		err = f.client.LoginByToken(ctx)
	}
	if err != nil {
		f.isAvailable = false
		if f.metrics != nil {
			f.metrics.TagFetchFailures.WithLabelValues(f.vcenterID).Inc()
		}
		return fmt.Errorf("%w: login failed: %v", ErrTaggingUnavailable, err)
	}

	// Preload category definitions
	cats, err := f.manager.GetCategories(ctx)
	if err != nil {
		f.isAvailable = false
		if f.metrics != nil {
			f.metrics.TagFetchFailures.WithLabelValues(f.vcenterID).Inc()
		}
		return fmt.Errorf("%w: failed to fetch categories: %v", ErrTaggingUnavailable, err)
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	for _, cat := range cats {
		f.categories[cat.ID] = cat.Name
	}

	// Preload tag definitions
	allTags, err := f.manager.GetTags(ctx)
	if err != nil {
		f.isAvailable = false
		if f.metrics != nil {
			f.metrics.TagFetchFailures.WithLabelValues(f.vcenterID).Inc()
		}
		return fmt.Errorf("%w: failed to fetch tags: %v", ErrTaggingUnavailable, err)
	}

	for _, t := range allTags {
		catName := f.categories[t.CategoryID]
		if catName == "" {
			catName = t.CategoryID
		}
		f.tagDefs[t.ID] = domain.TagRecord{
			TagID:        t.ID,
			CategoryName: catName,
			TagName:      t.Name,
		}
	}

	f.initialized = true
	f.isAvailable = true
	return nil
}

// FetchTagsForObjects fetches attached tags for the given object references in bounded batches.
func (f *RestTagFetcher) FetchTagsForObjects(
	ctx context.Context,
	refs []types.ManagedObjectReference,
) (map[string][]domain.TagRecord, error) {
	if !f.isAvailable || !f.initialized {
		return nil, ErrTaggingUnavailable
	}

	start := time.Now()
	res := make(map[string][]domain.TagRecord)
	if len(refs) == 0 {
		return res, nil
	}

	// Bounded batches
	for i := 0; i < len(refs); i += f.cfg.BatchSize {
		end := i + f.cfg.BatchSize
		if end > len(refs) {
			end = len(refs)
		}
		chunk := refs[i:end]

		moRefs := make([]mo.Reference, len(chunk))
		for j, r := range chunk {
			moRefs[j] = r
		}

		callCtx, cancel := context.WithTimeout(ctx, f.cfg.Timeout)
		attached, err := f.manager.ListAttachedTagsOnObjects(callCtx, moRefs)
		cancel()
		if err != nil {
			if f.metrics != nil {
				f.metrics.TagFetchFailures.WithLabelValues(f.vcenterID).Inc()
			}
			return nil, fmt.Errorf("%w: list attached tags failed: %v", ErrTaggingUnavailable, err)
		}

		f.mu.RLock()
		for _, att := range attached {
			moref := att.ObjectID.Reference().Value
			var tagRecs []domain.TagRecord
			for _, tagID := range att.TagIDs {
				if tagDef, ok := f.tagDefs[tagID]; ok {
					tagRecs = append(tagRecs, tagDef)
				} else {
					// Fallback for tags created after initial preload
					tagRecs = append(tagRecs, domain.TagRecord{
						TagID:        tagID,
						CategoryName: "Default",
						TagName:      tagID,
					})
				}
			}
			if len(tagRecs) > 0 {
				res[moref] = tagRecs
			}
		}
		f.mu.RUnlock()
	}

	if f.metrics != nil {
		f.metrics.TagFetchDuration.WithLabelValues(f.vcenterID).Observe(time.Since(start).Seconds())
	}

	return res, nil
}

// Close gracefully logs out and closes the REST session.
func (f *RestTagFetcher) Close(ctx context.Context) error {
	if f.client != nil && f.isAvailable {
		callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		_ = f.client.Logout(callCtx)
	}
	return nil
}
