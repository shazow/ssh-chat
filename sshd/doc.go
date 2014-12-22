package sshd

/*

	signer, err := ssh.ParsePrivateKey(privateKey)

	config := MakeNoAuth()
	config.AddHostKey(signer)

	s, err := ListenSSH("0.0.0.0:22", config)
	if err != nil {
		// Handle opening socket error
	}

	terminals := s.ServeTerminal()

	for term := range terminals {
		go func() {
			defer term.Close()
			term.SetPrompt("...")
			term.AutoCompleteCallback = nil // ...

			for {
				line, err := term.Readline()
				if err != nil {
					break
				}
				term.Write(...)
			}

		}()
	}
*/
