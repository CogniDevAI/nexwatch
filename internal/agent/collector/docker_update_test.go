package collector

import (
	"context"
	"errors"
	"testing"

	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/registry"
	"github.com/docker/docker/client"
	digest "github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// fakeImageDigestInspector is an imageDigestInspector test double: it
// returns configured responses/errors and counts DistributionInspect calls
// so cache-hit behavior can be asserted without a real daemon or registry.
type fakeImageDigestInspector struct {
	inspectResp image.InspectResponse
	inspectErr  error

	distResp registry.DistributionInspect
	distErr  error

	distCalls int
}

func (f *fakeImageDigestInspector) ImageInspect(ctx context.Context, imageID string, opts ...client.ImageInspectOption) (image.InspectResponse, error) {
	return f.inspectResp, f.inspectErr
}

func (f *fakeImageDigestInspector) DistributionInspect(ctx context.Context, imageRef, encodedRegistryAuth string) (registry.DistributionInspect, error) {
	f.distCalls++
	return f.distResp, f.distErr
}

func distInspectWithDigest(d string) registry.DistributionInspect {
	return registry.DistributionInspect{
		Descriptor: ocispec.Descriptor{Digest: digest.Digest(d)},
	}
}

func TestHasRegistryReference(t *testing.T) {
	tests := []struct {
		name string
		ref  string
		want bool
	}{
		{"normal tagged image", "nginx:alpine", true},
		{"registry with namespace", "ghcr.io/org/app:v1", true},
		{"empty", "", false},
		{"bare digest", "sha256:abcd1234", false},
		{"untagged locally built image", "<none>:<none>", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasRegistryReference(tt.ref); got != tt.want {
				t.Errorf("hasRegistryReference(%q) = %v, want %v", tt.ref, got, tt.want)
			}
		})
	}
}

func TestFirstDigest(t *testing.T) {
	tests := []struct {
		name        string
		repoDigests []string
		want        string
	}{
		{"empty list", nil, ""},
		{"single digest", []string{"nginx@sha256:abcd"}, "sha256:abcd"},
		{"multiple digests uses first", []string{"nginx@sha256:abcd", "nginx@sha256:efgh"}, "sha256:abcd"},
		{"no @ separator", []string{"nginx"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := firstDigest(tt.repoDigests); got != tt.want {
				t.Errorf("firstDigest(%v) = %q, want %q", tt.repoDigests, got, tt.want)
			}
		})
	}
}

func TestCheckImageUpdate_SkipsImagesWithoutRegistryReference(t *testing.T) {
	cli := &fakeImageDigestInspector{}
	cache := newImageDigestCache()

	result := checkImageUpdate(context.Background(), cli, cache, "img-id", "<none>:<none>")

	if result.Checked {
		t.Errorf("checkImageUpdate() for a locally-built image = %+v, want Checked=false", result)
	}
	if cli.distCalls != 0 {
		t.Error("checkImageUpdate() must never contact the registry for an image with no registry reference")
	}
}

func TestCheckImageUpdate_SkipsWhenLocalInspectFails(t *testing.T) {
	cli := &fakeImageDigestInspector{inspectErr: errors.New("no such image")}
	cache := newImageDigestCache()

	result := checkImageUpdate(context.Background(), cli, cache, "img-id", "nginx:alpine")

	if result.Checked {
		t.Errorf("checkImageUpdate() with a failed local inspect = %+v, want Checked=false", result)
	}
}

func TestCheckImageUpdate_SkipsWhenNoLocalRepoDigest(t *testing.T) {
	cli := &fakeImageDigestInspector{inspectResp: image.InspectResponse{RepoDigests: nil}}
	cache := newImageDigestCache()

	result := checkImageUpdate(context.Background(), cli, cache, "img-id", "nginx:alpine")

	if result.Checked {
		t.Errorf("checkImageUpdate() with no local RepoDigests = %+v, want Checked=false", result)
	}
	if cli.distCalls != 0 {
		t.Error("checkImageUpdate() must not contact the registry when there is no local digest to compare")
	}
}

func TestCheckImageUpdate_NoUpdateWhenDigestsMatch(t *testing.T) {
	cli := &fakeImageDigestInspector{
		inspectResp: image.InspectResponse{RepoDigests: []string{"nginx@sha256:same"}},
		distResp:    distInspectWithDigest("sha256:same"),
	}
	cache := newImageDigestCache()

	result := checkImageUpdate(context.Background(), cli, cache, "img-id", "nginx:alpine")

	if !result.Checked {
		t.Fatalf("checkImageUpdate() = %+v, want Checked=true", result)
	}
	if result.UpdateAvailable {
		t.Error("checkImageUpdate() with matching digests reported UpdateAvailable=true")
	}
	if result.RemoteDigest != "sha256:same" {
		t.Errorf("checkImageUpdate().RemoteDigest = %q, want %q", result.RemoteDigest, "sha256:same")
	}
}

func TestCheckImageUpdate_UpdateAvailableWhenDigestsDiffer(t *testing.T) {
	cli := &fakeImageDigestInspector{
		inspectResp: image.InspectResponse{RepoDigests: []string{"nginx@sha256:old"}},
		distResp:    distInspectWithDigest("sha256:new"),
	}
	cache := newImageDigestCache()

	result := checkImageUpdate(context.Background(), cli, cache, "img-id", "nginx:alpine")

	if !result.UpdateAvailable {
		t.Errorf("checkImageUpdate() with differing digests = %+v, want UpdateAvailable=true", result)
	}
}

func TestCheckImageUpdate_RegistryUnreachableStillReturnsLocalDigest(t *testing.T) {
	cli := &fakeImageDigestInspector{
		inspectResp: image.InspectResponse{RepoDigests: []string{"nginx@sha256:local"}},
		distErr:     errors.New("registry unreachable"),
	}
	cache := newImageDigestCache()

	result := checkImageUpdate(context.Background(), cli, cache, "img-id", "nginx:alpine")

	if !result.Checked {
		t.Fatalf("checkImageUpdate() with an unreachable registry = %+v, want Checked=true (local digest still reported)", result)
	}
	if result.LocalDigest != "sha256:local" {
		t.Errorf("checkImageUpdate().LocalDigest = %q, want %q", result.LocalDigest, "sha256:local")
	}
	if result.UpdateAvailable {
		t.Error("checkImageUpdate() with an unreachable registry must not report UpdateAvailable=true")
	}
	if result.RemoteDigest != "" {
		t.Errorf("checkImageUpdate().RemoteDigest = %q, want empty when the registry is unreachable", result.RemoteDigest)
	}
}

func TestImageDigestCache_CachesWithinTTL(t *testing.T) {
	cli := &fakeImageDigestInspector{distResp: distInspectWithDigest("sha256:cached")}
	cache := newImageDigestCache()

	digest1, err1 := cache.remoteDigest(context.Background(), cli, "nginx:alpine")
	digest2, err2 := cache.remoteDigest(context.Background(), cli, "nginx:alpine")

	if err1 != nil || err2 != nil {
		t.Fatalf("remoteDigest() errors = %v, %v, want nil", err1, err2)
	}
	if digest1 != "sha256:cached" || digest2 != "sha256:cached" {
		t.Errorf("remoteDigest() = %q, %q, want both %q", digest1, digest2, "sha256:cached")
	}
	if cli.distCalls != 1 {
		t.Errorf("DistributionInspect called %d times, want 1 (second call should hit the cache)", cli.distCalls)
	}
}

func TestImageDigestCache_DifferentImagesAreNotShared(t *testing.T) {
	cli := &fakeImageDigestInspector{distResp: distInspectWithDigest("sha256:x")}
	cache := newImageDigestCache()

	_, _ = cache.remoteDigest(context.Background(), cli, "nginx:alpine")
	_, _ = cache.remoteDigest(context.Background(), cli, "redis:latest")

	if cli.distCalls != 2 {
		t.Errorf("DistributionInspect called %d times for 2 distinct images, want 2", cli.distCalls)
	}
}
