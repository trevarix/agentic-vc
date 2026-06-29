// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package web serves a standalone web UI for browsing and managing AVC snapshots.
package web

import "embed"

//go:embed static/*
var staticFS embed.FS
