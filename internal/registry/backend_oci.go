// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/memory"
	"oras.land/oras-go/v2/registry/remote"
	orasauth "oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/credentials"
	"oras.land/oras-go/v2/registry/remote/retry"
)

type OCIBackend struct {
	newTarget OCITargetFactory
}

type OCITarget interface {
	content.Storage
	Tag(ctx context.Context, desc ocispec.Descriptor, reference string) error
	Resolve(ctx context.Context, reference string) (ocispec.Descriptor, error)
}

type OCITargetFactory func(ctx context.Context, uri URI, auth AuthConfig) (OCITarget, error)

func NewOCIBackend(factory OCITargetFactory) *OCIBackend {
	if factory == nil {
		factory = DefaultOCITargetFactory
	}
	return &OCIBackend{newTarget: factory}
}

func (b *OCIBackend) Scheme() Scheme { return SchemeOCI }

func (b *OCIBackend) Bundle(ctx context.Context, srcDir string, uri URI, auth AuthConfig) (string, error) {
	if err := requireScheme(uri, SchemeOCI); err != nil {
		return "", err
	}
	if uri.Ref == "" {
		return "", fmt.Errorf("bundling %q: pushing needs a tag (oci://host/repo:<tag>)", uri.String())
	}
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("bundling %q: %w", uri.String(), err)
	}
	blob, digestText, err := BundleBytes(srcDir)
	if err != nil {
		return "", fmt.Errorf("bundling %q: %w", uri.String(), err)
	}
	blobDesc, err := pushBlob(ctx, blob, digestText)
	if err != nil {
		return "", fmt.Errorf("bundling %q: %w", uri.String(), err)
	}
	store := blobDesc.store
	manifestDesc, err := oras.PackManifest(
		ctx, store, oras.PackManifestVersion1_1, BundleArtifactType,
		oras.PackManifestOptions{Layers: []ocispec.Descriptor{blobDesc.desc}},
	)
	if err != nil {
		return "", fmt.Errorf("bundling %q: packing manifest: %w", uri.String(), err)
	}
	if err := store.Tag(ctx, manifestDesc, uri.Ref); err != nil {
		return "", fmt.Errorf("bundling %q: tagging manifest: %w", uri.String(), err)
	}
	target, err := b.newTarget(ctx, uri, auth)
	if err != nil {
		return "", fmt.Errorf("bundling %q: %w", uri.String(), err)
	}
	if _, err := oras.Copy(ctx, store, uri.Ref, target, uri.Ref, oras.DefaultCopyOptions); err != nil {
		return "", fmt.Errorf("bundling %q: pushing artifact: %w", uri.String(), err)
	}
	return digestText, nil
}

func (b *OCIBackend) Fetch(ctx context.Context, uri URI, auth AuthConfig, destDir string) error {
	if err := requireScheme(uri, SchemeOCI); err != nil {
		return err
	}
	reference := uri.Ref
	if reference == "" {
		reference = uri.Digest
	}
	if reference == "" {
		return fmt.Errorf("fetching %q: oci uri needs a tag or digest", uri.String())
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("fetching %q: %w", uri.String(), err)
	}
	target, err := b.newTarget(ctx, uri, auth)
	if err != nil {
		return fmt.Errorf("fetching %q: %w", uri.String(), err)
	}
	store := memory.New()
	manifestDesc, err := oras.Copy(ctx, target, reference, store, reference, oras.DefaultCopyOptions)
	if err != nil {
		return fmt.Errorf("fetching %q: pulling artifact: %w", uri.String(), err)
	}
	blob, err := fetchLayer(ctx, store, manifestDesc)
	if err != nil {
		return fmt.Errorf("fetching %q: %w", uri.String(), err)
	}
	if err := ExtractBundle(bytes.NewReader(blob), destDir); err != nil {
		return fmt.Errorf("fetching %q: %w", uri.String(), err)
	}
	return nil
}

type pushedBlob struct {
	store *memory.Store
	desc  ocispec.Descriptor
}

func pushBlob(ctx context.Context, blob []byte, digestText string) (pushedBlob, error) {
	parsed, err := digest.Parse(digestText)
	if err != nil {
		return pushedBlob{}, fmt.Errorf("parsing bundle digest: %w", err)
	}
	desc := ocispec.Descriptor{
		MediaType: BundleMediaType,
		Digest:    parsed,
		Size:      int64(len(blob)),
	}
	store := memory.New()
	if err := store.Push(ctx, desc, bytes.NewReader(blob)); err != nil {
		return pushedBlob{}, fmt.Errorf("staging bundle blob: %w", err)
	}
	return pushedBlob{store: store, desc: desc}, nil
}

func fetchLayer(ctx context.Context, store *memory.Store, manifestDesc ocispec.Descriptor) ([]byte, error) {
	manifestJSON, err := content.FetchAll(ctx, store, manifestDesc)
	if err != nil {
		return nil, fmt.Errorf("reading artifact manifest: %w", err)
	}
	var manifest ocispec.Manifest
	if err := json.Unmarshal(manifestJSON, &manifest); err != nil {
		return nil, fmt.Errorf("decoding artifact manifest: %w", err)
	}
	if len(manifest.Layers) != 1 {
		return nil, fmt.Errorf("artifact holds %d layers, want exactly the template bundle", len(manifest.Layers))
	}
	layer := manifest.Layers[0]
	if layer.MediaType != BundleMediaType {
		return nil, fmt.Errorf("artifact layer is %q, want %q", layer.MediaType, BundleMediaType)
	}
	blob, err := content.FetchAll(ctx, store, layer)
	if err != nil {
		return nil, fmt.Errorf("reading template bundle: %w", err)
	}
	if got := digest.FromBytes(blob); got != layer.Digest {
		return nil, fmt.Errorf("template bundle digest mismatch: tampered or corrupt transfer")
	}
	return blob, nil
}

func DefaultOCITargetFactory(ctx context.Context, uri URI, auth AuthConfig) (OCITarget, error) {
	repo, err := remote.NewRepository(uri.Host + "/" + uri.Path)
	if err != nil {
		return nil, fmt.Errorf("opening repository %q: %w", uri.Host+"/"+uri.Path, err)
	}
	repo.Client = &orasauth.Client{
		Client:     retry.DefaultClient,
		Credential: ociCredentialFunc(ctx, uri.Host, auth),
		Cache:      orasauth.NewCache(),
	}
	return repo, nil
}

func ociCredentialFunc(ctx context.Context, host string, authCfg AuthConfig) orasauth.CredentialFunc {
	return func(_ context.Context, _ string) (orasauth.Credential, error) {
		switch authCfg.Kind {
		case AuthKindToken:
			if token, ok := authCfg.BearerToken(); ok {
				return orasauth.Credential{Username: ociUsername(authCfg), Password: token}, nil
			}
		case AuthKindBasic:
			if username, password, ok := authCfg.BasicAuth(); ok {
				return orasauth.Credential{Username: username, Password: password}, nil
			}
		}
		if store, err := credentials.NewStoreFromDocker(credentials.StoreOptions{}); err == nil {
			if cred, err := store.Get(ctx, host); err == nil && cred != orasauth.EmptyCredential {
				return cred, nil
			}
		}
		return orasauth.EmptyCredential, nil
	}
}

func ociUsername(authCfg AuthConfig) string {
	if authCfg.Username != "" {
		return authCfg.Username
	}
	return "turutan"
}
