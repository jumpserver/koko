package srvconn

func LoginToTelnetSu(sc *TelnetConnection) error {
	cfg := sc.cfg.suUserCfg
	sudoCommand := cfg.SuCommand()
	password := cfg.SuPassword()
	successPattern := cfg.SuccessPattern()
	passwordPattern := cfg.PasswordMatchPattern()
	steps := make([]stepItem, 0, 2)
	steps = append(steps,
		stepItem{
			Input:           sudoCommand,
			ExpectPattern:   passwordPattern,
			FinishedPattern: successPattern,
		},
		stepItem{
			Input:           password,
			ExpectPattern:   successPattern,
			FinishedPattern: successPattern,
		},
	)
	for i := 0; i < len(steps); i++ {
		finished, err := executeStep(&steps[i], sc)
		if err != nil {
			return err
		}
		if finished {
			break
		}
	}
	return nil
}
