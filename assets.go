package koko

import "embed"

//go:embed  static/*
var StaticFs embed.FS

//go:embed   ui/dist/*
var UIFs embed.FS

//go:embed  templates/*
var TemplateFs embed.FS
