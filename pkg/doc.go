// Package goversion bumps semantic versions and publishes tagged Go module releases.
//
// Run updates a Go version file, applies configured project-file changes, creates a version commit, and tags that commit locally.
// DryRun reports the version and files that Run would update without changing files or Git state.
// Publish validates an existing version commit and tag, publishes incomplete Git refs, creates or reuses a GitHub Release, and seeds the Go module proxy.
// Publish is resumable, so retrying after a failure reuses completed remote Git and GitHub Release steps.
package goversion
