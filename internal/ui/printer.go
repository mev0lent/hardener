package ui

import (
	"fmt"
	"time"
)

func PrintWelcome(mode, message string) {
	if emit(mode, message) {
		return
	}
	msg := fmt.Sprintf("[> %s] %s", mode, message)
	fmt.Println(Header.Render(msg))
}

func PrintHeader(message string) {
	if emit("suite", message) {
		return
	}
	fmt.Println(Header.Render("[> HEADER] " + message))
}

func PrintDebug(platform string, arch string) {
	if emit("debug", fmt.Sprintf("platform=%s arch=%s", platform, arch)) {
		return
	}
	msg := fmt.Sprintf("[%s] platform: os=%s arch=%s",
		time.Now().Format("2006-01-02 15:04:05"),
		platform, arch,
	)
	fmt.Println(LogBox.Render(msg))
}

func PrintDebugMessage(message string, platform string, arch string) {
	if emit("debug", message) {
		return
	}
	msg := fmt.Sprintf("[%s] platform: os=%s arch=%s\n[MESSAGE]: %s",
		time.Now().Format("2006-01-02 15:04:05"),
		platform, arch, message,
	)
	fmt.Println(LogBox.Render(msg))
}

func Error(err error) error {
	return fmt.Errorf("%s", ErrorBox.Render("[> ERROR] "+err.Error()))
}

func ReturnError(message string, error error) error {
	text := message
	if error != nil {
		if text != "" {
			text += ": "
		}
		text += error.Error()
	}
	if emit("error", text) {
		return error
	}

	if message != "" {
		fmt.Println(ErrorBox.Render("[> ERROR] " + message))
	} else {
		fmt.Println(ErrorBox.Render("[> ERROR] " + error.Error()))
	}
	return error
}

func PrintErrorMessage(message string) {
	if emit("error", message) {
		return
	}
	fmt.Println(ErrorBox.Render("[> ERROR] " + message))
}

func PrintSummary(message string) {
	if emit("summary", message) {
		return
	}
	fmt.Println(SummaryBox.Render(message))
}

func PrintInfo(message string) {
	if emit("info", message) {
		return
	}
	fmt.Println(Info("[> INFO] " + message + "\n"))
}

func PrintErrorSummary(message string, errs []error) {
	if emit("error", fmt.Sprintf("%s: %v", message, errs)) {
		return
	}

	result := fmt.Sprintf("")
	result += message + "\n:"
	for _, err := range errs {
		result += err.Error() + "\n"
	}
}

func PrintFinalInfo(message string) {
	if emit("summary", message) {
		return
	}
	fmt.Println(FinalInfo("[> SUMMARY] " + message))
}

func PrintSkipped(id string) {
	if emit("skip", id+" | higher security level or unsupported distro") {
		return
	}
	msg := fmt.Sprintf("[> SKIPPED]: %s | Test had a higher security level than asked for by user.\n", id)
	fmt.Println(Info(msg))
}

func PrintSkippedMissing(id, resource string) {
	if emit("skip", fmt.Sprintf("%s | required resource %q unavailable", id, resource)) {
		return
	}
	msg := fmt.Sprintf("[> SKIPPED]: %s | Required resource %q not present on this system.\n", id, resource)
	fmt.Println(Info(msg))
}

func PrintPassed(id string) {
	if emit("pass", id+" | check passed") {
		return
	}
	msg := fmt.Sprintf("[> PASSED]: %s | Test succeeded.\n", id)
	fmt.Println(Passed(msg))
}

func PrintFailed(id string, expected string, outcome, command string) {
	if emit("fail", fmt.Sprintf("%s | expected %s | got %s | %s", id, expected, outcome, command)) {
		return
	}
	msg := fmt.Sprintf("[> FAILED]: %s | Test failed, fix needed. \nDesired Output: %s \nOutput: %s \nUsed command: %s\n", id, expected, outcome, command)
	fmt.Println(Failed(msg))
}

func PrintFixed(message string) {
	if emit("fixed", message) {
		return
	}
	fmt.Println(Passed("[> FIXED] " + message + "\n"))
}

func PrintFailedAsBox(message string) {
	if emit("error", message) {
		return
	}
	fmt.Println(ErrorBox.Render(message + "\n"))
}

func PrintReport(message string) {
	if emit("report", message) {
		return
	}
	fmt.Println(Report(message))
}
