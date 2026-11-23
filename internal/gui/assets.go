package gui

import _ "embed"

// AppIconPNG is the 512x512 RGBA Tailfile icon embedded for window/dock badges.
//
//go:embed icon.png
var AppIconPNG []byte

// ThirdPartyNotices documents icon license attributions for bundled UI marks.
const ThirdPartyNotices = `Lucide Icons — ISC License
Copyright (c) 2026 Lucide Icons and Contributors

Feather-derived Lucide icons — MIT License
Copyright (c) 2013-present Cole Bemis
`
