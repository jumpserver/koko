package koko

import (
	"embed"
	"io/fs"
)

//go:embed  static/*
var StaticFs embed.FS

//go:embed all:ui_embed
var uiFS embed.FS

var UIFs, _ = fs.Sub(uiFS, "ui_embed")

//go:embed  templates/*
var TemplateFs embed.FS
