package koko

import "embed"

//go:embed  static/*
var StaticFs embed.FS

//go:embed  templates/*
var TemplateFs embed.FS
