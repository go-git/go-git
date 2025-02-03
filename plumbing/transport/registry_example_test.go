package transport

func ExampleInstallProtocol() {
	// Install it as default client for https URLs.
	Register("https", &dummyClient{})
}
