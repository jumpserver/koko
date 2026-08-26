package session

import (
	"github.com/spf13/viper"

	"github.com/jumpserver/koko/pkg/lion/guacd"
)

type valueType string

const (
	Boolean valueType = "boolean"
	String  valueType = "string"
	Integer valueType = "integer"
)

type DisplayParameter struct {
	Key          string
	DefaultValue string
	valueType    valueType
}

var (
	colorDepth               = DisplayParameter{Key: guacd.RDPColorDepth, DefaultValue: "24", valueType: Integer}
	dpi                      = DisplayParameter{Key: guacd.RDPDpi, DefaultValue: "", valueType: Integer}
	disableAudio             = DisplayParameter{Key: guacd.RDPDisableAudio, DefaultValue: "", valueType: Boolean}
	enableWallpaper          = DisplayParameter{Key: guacd.RDPEnableWallpaper, DefaultValue: "", valueType: Boolean}
	enableTheming            = DisplayParameter{Key: guacd.RDPEnableTheming, DefaultValue: "", valueType: Boolean}
	enableFontSmoothing      = DisplayParameter{Key: guacd.RDPEnableFontSmoothing, DefaultValue: "", valueType: Boolean}
	enableFullWindowDrag     = DisplayParameter{Key: guacd.RDPEnableFullWindowDrag, DefaultValue: "", valueType: Boolean}
	enableDesktopComposition = DisplayParameter{Key: guacd.RDPEnableDesktopComposition, DefaultValue: "", valueType: Boolean}
	enableMenuAnimations     = DisplayParameter{Key: guacd.RDPEnableMenuAnimations, DefaultValue: "", valueType: Boolean}
	disableBitmapCaching     = DisplayParameter{Key: guacd.RDPDisableBitmapCaching, DefaultValue: "", valueType: Boolean}
	disableOffscreenCaching  = DisplayParameter{Key: guacd.RDPDisableOffscreenCaching, DefaultValue: "", valueType: Boolean}
	vncCursorRender          = DisplayParameter{Key: guacd.VNCCursor, DefaultValue: "", valueType: String}
	enableConsoleAudio       = DisplayParameter{Key: guacd.RDPConsoleAudio, DefaultValue: "", valueType: Boolean}
	enableAudioInput         = DisplayParameter{Key: guacd.RDPEnableAudioInput, DefaultValue: "", valueType: Boolean}
)

type Display struct {
	data map[string]DisplayParameter
}

func (d Display) GetDisplayParams() map[string]string {
	res := make(map[string]string)
	for envKey, displayParam := range d.data {
		res[displayParam.Key] = displayParam.DefaultValue
		if value := viper.GetString(envKey); value != "" {
			switch displayParam.valueType {
			case Boolean:
				booleanValue := viper.GetBool(envKey)
				res[displayParam.Key] = ConvertBoolToString(booleanValue)
			default:
				res[displayParam.Key] = value
			}
		}
	}
	return res
}

var RDPDisplay = Display{data: map[string]DisplayParameter{
	"JUMPSERVER_COLOR_DEPTH":                colorDepth,
	"JUMPSERVER_DPI":                        dpi,
	"JUMPSERVER_DISABLE_AUDIO":              disableAudio,
	"JUMPSERVER_ENABLE_WALLPAPER":           enableWallpaper,
	"JUMPSERVER_ENABLE_THEMING":             enableTheming,
	"JUMPSERVER_ENABLE_FONT_SMOOTHING":      enableFontSmoothing,
	"JUMPSERVER_ENABLE_FULL_WINDOW_DRAG":    enableFullWindowDrag,
	"JUMPSERVER_ENABLE_DESKTOP_COMPOSITION": enableDesktopComposition,
	"JUMPSERVER_ENABLE_MENU_ANIMATIONS":     enableMenuAnimations,
	"JUMPSERVER_DISABLE_BITMAP_CACHING":     disableBitmapCaching,
	"JUMPSERVER_DISABLE_OFFSCREEN_CACHING":  disableOffscreenCaching,
	"JUMPSERVER_ENABLE_CONSOLE_AUDIO":       enableConsoleAudio,
	"JUMPSERVER_ENABLE_AUDIO_INPUT":         enableAudioInput,
}}

var VNCDisplay = Display{data: map[string]DisplayParameter{
	"JUMPSERVER_COLOR_DEPTH":       colorDepth,
	"JUMPSERVER_VNC_CURSOR_RENDER": vncCursorRender,
}}

var RDPBuiltIn = map[string]string{
	guacd.RDPDisableGlyphCaching: BoolTrue,
}
