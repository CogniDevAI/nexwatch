package collector

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/registry"
	"github.com/docker/docker/client"
)

// imageDigestInspector is the subset of the Docker client needed to detect
// image updates: reading a locally cached image's RepoDigests and asking
// the registry for the current manifest digest of the same reference. It
// is a narrower interface than the full Docker SDK client so tests can
// supply a fake without a real daemon or registry. *client.Client already
// implements both methods with these exact signatures.
type imageDigestInspector interface {
	ImageInspect(ctx context.Context, imageID string, opts ...client.ImageInspectOption) (image.InspectResponse, error)
	DistributionInspect(ctx context.Context, imageRef, encodedRegistryAuth string) (registry.DistributionInspect, error)
}

// imageDigestCacheTTL bounds how often the registry is contacted for the
// same image reference — update checks run every collection cycle, but a
// registry lookup is comparatively expensive and slow-changing.
const imageDigestCacheTTL = 1 * time.Hour

// imageDigestCacheEntry is one cached registry lookup result.
type imageDigestCacheEntry struct {
	remoteDigest string
	err          error
	checkedAt    time.Time
}

// imageUpdateInfo is the per-container result of an image update check.
// Checked is false when the image has no registry reference to compare
// against (e.g. a locally-built image) — in that case the caller must not
// set any of the docker_containers update-detection fields at all.
type imageUpdateInfo struct {
	LocalDigest     string
	RemoteDigest    string
	UpdateAvailable bool
	Checked         bool
}

// hasRegistryReference reports whether imageRef looks like something that
// could plausibly be resolved against a registry, as opposed to a bare
// digest or an untagged locally-built image (Docker reports these as
// "<none>:<none>").
func hasRegistryReference(imageRef string) bool {
	if imageRef == "" {
		return false
	}
	if strings.HasPrefix(imageRef, "sha256:") {
		return false
	}
	if strings.Contains(imageRef, "<none>") {
		return false
	}
	return true
}

// firstDigest extracts the digest portion (after "@") of the first
// populated entry of a RepoDigests list, e.g. "nginx@sha256:abcd..." ->
// "sha256:abcd...". Returns "" when repoDigests is empty or has no "@"
// separator (locally-built images never have RepoDigests populated at
// all, which is exactly the "no registry reference" case this is meant to
// detect).
func firstDigest(repoDigests []string) string {
	for _, rd := range repoDigests {
		if idx := strings.LastIndex(rd, "@"); idx != -1 {
			return rd[idx+1:]
		}
	}
	return ""
}

// checkImageUpdate compares imageID's locally recorded RepoDigest against
// the registry's current manifest digest for imageRef, using cache to
// cache remote lookups for imageDigestCacheTTL. It never returns an error:
// any failure (no registry reference, local inspect failure, unreachable
// registry) is reported by leaving Checked false or RemoteDigest empty and
// logged at a level that never interrupts the rest of the collection —
// image update detection is a best-effort enrichment, not a required fact.
func checkImageUpdate(ctx context.Context, cli imageDigestInspector, cache *imageDigestCache, imageID, imageRef string) imageUpdateInfo {
	if !hasRegistryReference(imageRef) {
		return imageUpdateInfo{}
	}

	info, err := cli.ImageInspect(ctx, imageID)
	if err != nil {
		log.Printf("[docker] update_check_error: image inspect for %s: %v", imageRef, err)
		return imageUpdateInfo{}
	}

	localDigest := firstDigest(info.RepoDigests)
	if localDigest == "" {
		// No repo digest recorded locally — e.g. a locally-built image
		// with no registry reference. Nothing to compare against.
		return imageUpdateInfo{}
	}

	remoteDigest, err := cache.remoteDigest(ctx, cli, imageRef)
	if err != nil {
		log.Printf("[docker] update_check_error: distribution inspect for %s: %v", imageRef, err)
		return imageUpdateInfo{LocalDigest: localDigest, Checked: true}
	}

	return imageUpdateInfo{
		LocalDigest:     localDigest,
		RemoteDigest:    remoteDigest,
		UpdateAvailable: remoteDigest != "" && remoteDigest != localDigest,
		Checked:         true,
	}
}

// imageDigestCache memoizes registry digest lookups per image reference
// for imageDigestCacheTTL, so a busy host with a short collection interval
// doesn't hammer its registries on every cycle.
type imageDigestCache struct {
	mu      sync.Mutex
	entries map[string]imageDigestCacheEntry
}

func newImageDigestCache() *imageDigestCache {
	return &imageDigestCache{entries: make(map[string]imageDigestCacheEntry)}
}

// remoteDigest returns the registry's current manifest digest for
// imageRef, using a cached value when it is younger than
// imageDigestCacheTTL and refreshing it (successful or not) otherwise.
func (c *imageDigestCache) remoteDigest(ctx context.Context, cli imageDigestInspector, imageRef string) (string, error) {
	c.mu.Lock()
	entry, ok := c.entries[imageRef]
	c.mu.Unlock()
	if ok && time.Since(entry.checkedAt) < imageDigestCacheTTL {
		return entry.remoteDigest, entry.err
	}

	dist, err := cli.DistributionInspect(ctx, imageRef, "")
	var digest string
	if err == nil {
		digest = string(dist.Descriptor.Digest)
	}

	c.mu.Lock()
	c.entries[imageRef] = imageDigestCacheEntry{remoteDigest: digest, err: err, checkedAt: time.Now()}
	c.mu.Unlock()

	return digest, err
}
