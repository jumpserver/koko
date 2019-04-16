package parser

type SpecialRuler interface {

	// 匹配规则
	MatchRule([]byte) bool

	// 进入状态
	EnterStatus() bool

	// 退出状态
	ExitStatus() bool
}
