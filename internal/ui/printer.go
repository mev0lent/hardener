package ui

import (
	"fmt"
	"time"
)

func PrintWelcome(mode, message string) {
	msg := fmt.Sprintf("[> %s] %s", mode, message)
	fmt.Println(Header.Render(msg))
}

func PrintHeader(message string) {
	fmt.Println(Header.Render("[> HEADER] " + message))
}

func PrintDebug(platform string, arch string) {
	msg := fmt.Sprintf("[%s] platform: os=%s arch=%s",
		time.Now().Format("2006-01-02 15:04:05"),
		platform, arch,
	)
	fmt.Println(LogBox.Render(msg))
}

func PrintDebugMessage(message string, platform string, arch string) {
	msg := fmt.Sprintf("[%s] platform: os=%s arch=%s\n[MESSAGE]: %s",
		time.Now().Format("2006-01-02 15:04:05"),
		platform, arch, message,
	)
	fmt.Println(LogBox.Render(msg))
}

func Error(err error) error {
	return fmt.Errorf(ErrorBox.Render("[> ERROR] " + err.Error()))
}

func ReturnError(message string, error error) error {
	if message != "" {
		fmt.Println(ErrorBox.Render("[> ERROR] " + message))
	} else {
		fmt.Println(ErrorBox.Render("[> ERROR] " + error.Error()))
	}
	return error
}

func PrintErrorMessage(message string) {
	fmt.Println(ErrorBox.Render("[> ERROR] " + message))
}

func PrintSummary(message string) {
	fmt.Println(SummaryBox.Render(message))
}

func PrintInfo(message string) {
	fmt.Println(Info("[> INFO] " + message + "\n"))
}

func PrintErrorSummary(message string, errs []error) {
	result := fmt.Sprintf("")
	result += message + "\n:"
	for _, err := range errs {
		result += err.Error() + "\n"
	}
}

func PrintFinalInfo(message string) {
	fmt.Println(FinalInfo("[> SUMMARY] " + message))
}

func PrintSkipped(id string) {
	msg := fmt.Sprintf("[> SKIPPED]: %s | Test had a higher security level than asked for by user.\n", id)
	fmt.Println(Info(msg))
}

func PrintPassed(id string) {
	msg := fmt.Sprintf("[> PASSED]: %s | Test succeeded.\n", id)
	fmt.Println(Passed(msg))
}

func PrintFailed(id string, expected string, outcome, command string) {
    msg := fmt.Sprintf("[> FAILED]: %s | Test failed, fix needed. \nDesired Output: %s \nOutput: %s \nUsed command: %s\n", id, expected, outcome, command)
    fmt.Println(Failed(msg))
}

func PrintFixed(message string) {
	fmt.Println(Passed("[> FIXED] " + message + "\n"))
}

func PrintFailedAsBox(message string) {
	fmt.Println(ErrorBox.Render(message + "\n"))
}

func PrintReport(message string) {
	fmt.Println(Report(message))
}
