package model

type Permission struct {
	Actions []string `json:"actions"`
}

func (p *Permission) EnableConnect() bool {
	return p.haveAction(ActionConnect)
}

func (p *Permission) EnableDrive() bool {
	return p.EnableDownload() || p.EnableUpload()
}

func (p *Permission) EnableDownload() bool {
	return p.haveAction(ActionDownload)
}

func (p *Permission) EnableUpload() bool {
	return p.haveAction(ActionUpload)
}

func (p *Permission) EnableCopy() bool {
	return p.haveAction(ActionCopy)
}

func (p *Permission) EnablePaste() bool {
	return p.haveAction(ActionPaste)
}

func (p *Permission) EnableDelete() bool {
	return p.haveAction(ActionDelete)
}

func (p *Permission) EnableShare() bool {
	return p.haveAction(ActionShare)
}

func (p *Permission) haveAction(action string) bool {
	for _, value := range p.Actions {
		if action == ActionALL || action == value {
			return true
		}
	}
	return false
}

const (
	ActionALL      = "all"
	ActionConnect  = "connect"
	ActionUpload   = "upload"
	ActionDownload = "download"
	ActionCopy     = "copy"
	ActionPaste    = "paste"
	ActionDelete   = "delete"
	ActionShare    = "share"
)

type ValidateResult struct {
	Ok  bool   `json:"ok"`
	Msg string `json:"msg"`
	Err string `json:"error"`
}
