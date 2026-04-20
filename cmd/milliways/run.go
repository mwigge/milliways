package main

// Run executes milliways with the provided CLI arguments.
func Run(args []string) error {
	cmd := rootCmd()
	cmd.SetArgs(args)
	return cmd.Execute()
}
