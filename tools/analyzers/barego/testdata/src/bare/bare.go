package bare

func Fire() {
	go work() // want `bare go statement`
}

func FireLiteral() {
	go func() { // want `bare go statement`
		work()
	}()
}

func work() {}
