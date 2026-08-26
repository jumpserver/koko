package guacd

import (
	"os"
	"strings"
	"time"
)

const (
	defaultOptimalScreenWidth  = 1024
	defaultOptimalScreenHeight = 768
	defaultOptimalResolution   = 96

	defaultTimezone = "Asia/Shanghai"
)

func getDefaultTimezone() string {
	timezone := strings.TrimSpace(os.Getenv("TZ"))
	if isValidTimezone(timezone) {
		return timezone
	}
	return defaultTimezone
}

func isValidTimezone(timezone string) bool {
	if timezone == "" {
		return false
	}
	_, err := time.LoadLocation(timezone)
	return err == nil
}

func NewClientInformation() ClientInformation {
	return ClientInformation{
		OptimalScreenWidth:  defaultOptimalScreenWidth,
		OptimalScreenHeight: defaultOptimalScreenHeight,
		OptimalResolution:   defaultOptimalResolution,
		Timezone:            getDefaultTimezone(),
		AudioMimetypes:      []string{"audio/L8", "audio/L16"},
		ImageMimetypes:      []string{"image/jpeg", "image/png", "image/webp"},
		VideoMimetypes:      []string{},
	}
}

type ClientInformation struct {
	/**
	 * The optimal screen width requested by the client, in pixels.
	 */
	OptimalScreenWidth int

	/**
	 * The optimal screen height requested by the client, in pixels.
	 */
	OptimalScreenHeight int

	/**
	 * The resolution of the optimal dimensions given, in DPI.
	 */
	OptimalResolution int

	/**
	 * The list of audio mimetypes reported by the client to be supported.
	 */
	AudioMimetypes []string

	/**
	 * The list of video mimetypes reported by the client to be supported.
	 */
	VideoMimetypes []string

	/**
	 * The list of image mimetypes reported by the client to be supported.
	 */
	ImageMimetypes []string

	/**
	 * The timezone reported by the client.
	 */
	Timezone string

	/**
	 * qwerty keyboard layout
	 */
	KeyboardLayout string
}

func (info *ClientInformation) SetTimezone(timezone string) {
	timezone = strings.TrimSpace(timezone)
	if isValidTimezone(timezone) {
		info.Timezone = timezone
	}
}

func (info *ClientInformation) ExtraConfig() map[string]string {
	ret := make(map[string]string)
	if layout, ok := RDPServerLayouts[info.KeyboardLayout]; ok {
		ret[RDPServerLayout] = layout
	}
	return ret
}

func (info *ClientInformation) Clone() ClientInformation {
	return ClientInformation{
		OptimalScreenWidth:  info.OptimalScreenWidth,
		OptimalScreenHeight: info.OptimalScreenHeight,
		OptimalResolution:   info.OptimalResolution,
		ImageMimetypes:      []string{"image/jpeg", "image/png", "image/webp"},
		Timezone:            info.Timezone,
		KeyboardLayout:      info.KeyboardLayout,
	}
}
