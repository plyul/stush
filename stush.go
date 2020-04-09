package main

import (
	"bytes"
	"flag"
	"fmt"
	color "github.com/logrusorgru/aurora"
	"io/ioutil"
	"log"
	"log/syslog"
	"net/url"
	"os"
	"os/exec"
	"os/user"
	"path"
	"regexp"
	"strings"
	"text/template"
)

// Maybe we should not hardcode this...
const (
	sshClientBinary    = "/usr/bin/ssh"
	telnetClientBinary = "/usr/bin/telnet"
)

// Utility structure for paths manipulation
type pathData struct {
	XDGDataHome          string
	XDGConfigHome        string
	Username             string
	AppName              string
	AppFilePath          string
	DesktopFilePath      string
	MimeappsListFilePath string
	HandlerDescriptors   string
}

const (
	xdgDefaultDataHomeTemplate   = "/home/{{.Username}}/.local"
	xdgDefaultConfigHomeTemplate = "/home/{{.Username}}/.config"
	desktopFilePathTemplate      = "{{.XDGDataHome}}/share/applications/{{.AppName}}.desktop"
	appFilePathTemplate          = "{{.XDGDataHome}}/bin/{{.AppName}}"
	mimeappsListFilePathTemplate = "{{.XDGConfigHome}}/mimeapps.list"
	handlerDescriptorsTemplate   = "x-scheme-handler/ssh={{.AppName}}.desktop\nx-scheme-handler/telnet={{.AppName}}.desktop"
	desktopFileContentTemplate   = `[Desktop Entry]
Version=1.0
Name=URL handler for SSH & Telnet schemes
Type=Application
Exec={{.AppFilePath}} --url=%u
MimeType=x-scheme-handler/ssh;x-scheme-handler/telnet;
Terminal=true
Icon=utilities-terminal
`
)

func main() {
	appName := os.Args[0]
	s, err := syslog.New(syslog.LOG_USER|syslog.LOG_DEBUG, appName)
	if err != nil {
		return
	}
	l := log.New(s, "", 0)

	var targetURL = flag.String("url", "", "url to connect in canonical form scheme://username@host:port/?option1=value;option2;option3")
	var doInstall = flag.Bool("install", false, "install handler (for current user)")
	var doRemove = flag.Bool("remove", false, "remove handler from the system (for current user)")
	flag.Parse()

	if *doInstall {
		if err := installHandler(path.Base(appName)); err != nil {
			fmt.Printf("Install failed: %s\n", err)
		}
		return
	}

	if *doRemove {
		if err := removeHandler(path.Base(appName)); err != nil {
			fmt.Printf("Remove failed: %s\n", err)
		}
		return
	}

	if len(*targetURL) == 0 {
		flag.Usage()
		return
	}
	l.Printf("Target URL: %s", *targetURL)
	u, err := url.Parse(*targetURL)
	if err != nil {
		l.Print("Error parsing URL")
		return
	}
	protocol := strings.ToLower(u.Scheme)
	switch protocol {
	case "ssh":
		port := u.Port()
		if port == "" {
			port = "22"
		}
		sshArgs := []string{"-p", port}
		sshArgs = append(sshArgs, argsFromQuery(u.Query())...)
		sshArgs = append(sshArgs, u.User.Username()+"@"+u.Hostname())
		executeClientApp(l, sshClientBinary, sshArgs)
	case "telnet":
		port := u.Port()
		if port == "" {
			port = "23"
		}
		telnetArgs := []string{"-l", u.User.Username()}
		telnetArgs = append(telnetArgs, argsFromQuery(u.Query())...)
		telnetArgs = append(telnetArgs, u.Hostname(), port)
		executeClientApp(l, telnetClientBinary, telnetArgs)
	default:
		l.Printf("Unknown protocol '%s'", protocol)
	}
	l.Print("Finished")
}

func executeClientApp(l *log.Logger, app string, args []string) {
	l.Printf("Executing %s %s", app, args)
	c := exec.Command(app, args...)
	c.Stdin = os.Stdout
	c.Stdout = os.Stdin
	var stderrBuf bytes.Buffer
	c.Stderr = &stderrBuf
	err := c.Run()
	if err != nil {
		l.Printf("Error running command: %s", err)
		l.Print(stderrBuf.String())
	}
}

func argsFromQuery(values url.Values) []string {
	var args []string
	for arg, argValues := range values {
		for _, argValue := range argValues {
			args = append(args, "-"+arg)
			if len(argValue) > 0 {
				args = append(args, argValue)
			}
		}
	}
	return args
}

func installHandler(appName string) error {
	pd, err := preparePathData(appName)
	if err != nil {
		return err
	}

	fmt.Printf("Writing handler executable to %s... ", pd.AppFilePath)
	callAndPrintResult(func() error { return copyHandlerExecutable(pd.AppFilePath) })

	fmt.Printf("Writing .desktop file to %s... ", pd.DesktopFilePath)
	callAndPrintResult(func() error { return writeDesktopFile(pd) })

	fmt.Print("Registering handlers... ")
	callAndPrintResult(func() error { return addHandlersToMIMEAppsList(pd) })

	return nil
}

func removeHandler(appName string) error {
	pd, err := preparePathData(appName)
	if err != nil {
		return err
	}
	// Unregister handlers
	fmt.Print("Unregistering handlers... ")
	callAndPrintResult(func() error { return removeHandlersFromMIMEAppsList(pd) })

	// Delete .desktop file
	fmt.Printf("Deleting .desktop file %s... ", pd.DesktopFilePath)
	callAndPrintResult(func() error { return os.Remove(pd.DesktopFilePath) })

	// Delete handler executable
	fmt.Printf("Deleting handler executable %s... ", pd.AppFilePath)
	callAndPrintResult(func() error { return os.Remove(pd.AppFilePath) })

	return nil
}

func callAndPrintResult(f func() error) {
	if err := f(); err != nil {
		fmt.Println(color.Red(fmt.Sprintf("failed (%s)", err.Error())))
	} else {
		fmt.Println(color.Green("ok"))
	}
}

func renderTemplateString(text string, data interface{}) (string, error) {
	t, err := template.New("t").Parse(text)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	err = t.Execute(&buf, data)
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

func preparePathData(appName string) (pathData, error) {
	pd := pathData{}
	currentUser, err := user.Current()
	if err != nil {
		return pd, err
	}
	pd.Username = currentUser.Username
	pd.AppName = appName
	pd.XDGDataHome, err = renderTemplateString(xdgDataHomeTemplate(), pd)
	if err != nil {
		return pd, err
	}
	pd.XDGConfigHome, err = renderTemplateString(xdgConfigHomeTemplate(), pd)
	if err != nil {
		return pd, err
	}
	pd.AppFilePath, err = renderTemplateString(appFilePathTemplate, pd)
	if err != nil {
		return pd, err
	}
	pd.DesktopFilePath, err = renderTemplateString(desktopFilePathTemplate, pd)
	if err != nil {
		return pd, err
	}
	pd.MimeappsListFilePath, err = renderTemplateString(mimeappsListFilePathTemplate, pd)
	if err != nil {
		return pd, err
	}
	pd.HandlerDescriptors, err = renderTemplateString(handlerDescriptorsTemplate, pd)
	if err != nil {
		return pd, err
	}

	return pd, nil
}

func copyHandlerExecutable(destination string) error {
	if err := os.MkdirAll(path.Dir(destination), 0700); err != nil {
		return err
	}
	me, err := os.Executable()
	if err != nil {
		return err
	}
	appBytes, err := ioutil.ReadFile(me)
	if err != nil {
		return err
	}
	if err := ioutil.WriteFile(destination, appBytes, 0755); err != nil {
		return err
	}
	return nil
}

func writeDesktopFile(pd pathData) error {
	if err := os.MkdirAll(path.Dir(pd.DesktopFilePath), 0700); err != nil {
		return err
	}
	desktopFileContent, err := renderTemplateString(desktopFileContentTemplate, pd)
	if err != nil {
		return err
	}
	if err := ioutil.WriteFile(pd.DesktopFilePath, []byte(desktopFileContent), 0644); err != nil {
		return err
	}
	return nil
}

func addHandlersToMIMEAppsList(pd pathData) error {
	if err := removeHandlersFromMIMEAppsList(pd); err != nil { // prevent adding of multiple handlers
		return err
	}
	hdr := "[Default Applications]"
	r := regexp.MustCompile(`\` + hdr)
	mimeappsListContent, err := ioutil.ReadFile(pd.MimeappsListFilePath)
	if err != nil {
		return err
	}
	newMimeappsListContent := r.ReplaceAll(mimeappsListContent, []byte(hdr+"\n"+pd.HandlerDescriptors))
	if err := ioutil.WriteFile(pd.MimeappsListFilePath, newMimeappsListContent, 0644); err != nil {
		return err
	}
	return nil
}

func removeHandlersFromMIMEAppsList(pd pathData) error {
	r := regexp.MustCompile(regexp.QuoteMeta(pd.HandlerDescriptors) + "\n")
	mimeappsListContent, err := ioutil.ReadFile(pd.MimeappsListFilePath)
	if err != nil {
		return err
	}
	newMimeappsListContent := r.ReplaceAll(mimeappsListContent, []byte(""))
	if err := ioutil.WriteFile(pd.MimeappsListFilePath, newMimeappsListContent, 0644); err != nil {
		return err
	}
	return nil
}

func xdgDataHomeTemplate() string {
	v := os.Getenv("XDG_DATA_HOME")
	if len(v) == 0 {
		v = xdgDefaultDataHomeTemplate
	}
	return v
}

func xdgConfigHomeTemplate() string {
	v := os.Getenv("XDG_CONFIG_HOME")
	if len(v) == 0 {
		v = xdgDefaultConfigHomeTemplate
	}
	return v
}
