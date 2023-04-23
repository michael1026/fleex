package scan

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hnakamur/go-scp"

	"github.com/michael1026/fleex/pkg/controller"
	p "github.com/michael1026/fleex/pkg/provider"
	"github.com/michael1026/fleex/pkg/sshutils"
	"github.com/michael1026/fleex/pkg/utils"
)

func lineCounter(r io.Reader) (int, error) {
	buf := make([]byte, 32*1024)
	count := 0
	lineSep := []byte{'\n'}

	for {
		c, err := r.Read(buf)
		count += bytes.Count(buf[:c], lineSep)

		switch {
		case err == io.EOF:
			return count, nil

		case err != nil:
			return count, err
		}
	}
}

func GetLine(filename string, names chan string, readerr chan error) error {
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		names <- scanner.Text()
	}
	readerr <- scanner.Err()

	return nil
}

// Start runs a scan
func Start(fleetName, command string, delete bool, input, outputPath, chunksFolder string, token string, port int, username, password string, provider controller.Provider) error {
	var isFolderOut bool
	start := time.Now()

	timeStamp := strconv.FormatInt(time.Now().UnixNano(), 10)
	// TODO: use a proper temp folder function so that it can run on windows too
	tempFolder := filepath.Join("/tmp", "fleex-"+timeStamp)

	if chunksFolder != "" {
		tempFolder = chunksFolder
	}

	// Make local temp folder
	tempFolderInput := filepath.Join(tempFolder, "input")
	// Create temp folder
	utils.MakeFolder(tempFolder)
	utils.MakeFolder(tempFolderInput)
	utils.Log.Info("Scan started!")

	// Input file to string

	fleet := controller.GetFleet(fleetName, token, provider)
	if len(fleet) < 1 {
		return fmt.Errorf("No fleet found")
	}

	/////

	// First get lines count
	file, err := os.Open(input)

	if err != nil {
		return err
	}

	linesCount, err := lineCounter(file)

	if err != nil {
		return err
	}

	utils.Log.Debug("Fleet count: ", len(fleet))
	linesPerChunk := linesCount / len(fleet)
	linesPerChunkRest := linesCount % len(fleet)

	names := make(chan string)
	readerr := make(chan error)

	go GetLine(input, names, readerr)
	counter := 1
	asd := []string{}

	x := 1
loop:
	for {
		select {
		case name := <-names:
			// Process each line
			asd = append(asd, name)

			re := 0

			if linesPerChunkRest > 0 {
				re = 1
			}
			if counter%(linesPerChunk+re) == 0 {
				utils.StringToFile(filepath.Join(tempFolderInput, "chunk-"+fleetName+"-"+strconv.Itoa(x)), strings.Join(asd[0:counter], "\n")+"\n")
				asd = nil
				x++
				counter = 0
				linesPerChunkRest--

			}
			counter++

		case err := <-readerr:
			if err != nil {
				return err
			}
			break loop
		}
	}

	utils.Log.Debug("Generated file chunks")

	/////

	fleetNames := make(chan *p.Box, len(fleet))
	processGroup := new(sync.WaitGroup)
	processGroup.Add(len(fleet))

	for i := 0; i < len(fleet); i++ {
		go func() error {
			for {
				l := <-fleetNames
				if l == nil {
					break
				}
				boxName := l.Label

				// Send input file via SCP
				err := scp.NewSCP(sshutils.GetConnection(l.IP, port, username, password).Client).SendFile(filepath.Join(tempFolderInput, "chunk-"+boxName), "/tmp/fleex-"+timeStamp+"-chunk-"+boxName)
				if err != nil {
					return err
				}

				chunkInputFile := "/tmp/fleex-" + timeStamp + "-chunk-" + boxName
				chunkOutputFile := "/tmp/fleex-" + timeStamp + "-chunk-out-" + boxName

				// Replace labels and craft final command
				finalCommand := command
				finalCommand = strings.ReplaceAll(finalCommand, "{{INPUT}}", chunkInputFile)
				finalCommand = strings.ReplaceAll(finalCommand, "{{OUTPUT}}", chunkOutputFile)

				sshutils.RunCommand(finalCommand, l.IP, port, username, password)

				// Now download the output file
				//utils.MakeFolder(filepath.Join(tempFolder, "chunk-out-"+boxName))
				isFolderOut, _ = SendSCP(chunkOutputFile, filepath.Join(tempFolder, "chunk-out-"+boxName), l.IP, port, username, password)

				// err = scp.NewSCP(sshutils.GetConnection(l.IP, port, username, password).Client).ReceiveFile(chunkOutputFile, filepath.Join(tempFolder, "chunk-out-"+boxName))
				// if err != nil {
				// 	utils.Log.Fatal("Failed to get file: ", err)
				// }

				if delete {
					// TODO: Not the best way to delete a box, if this program crashes/is stopped
					// before reaching this line the box won't be deleted. It's better to setup
					// a cron/command on the box directly.
					controller.DeleteBoxByID(l.ID, token, provider)
					utils.Log.Debug("Killed box ", l.Label)
				}

			}
			processGroup.Done()
			return nil
		}()
	}

	for i := range fleet {
		fleetNames <- &fleet[i]
	}

	close(fleetNames)
	processGroup.Wait()

	if err != nil {
		return err
	}

	// Scan done, process results
	duration := time.Since(start)
	utils.Log.Info("Scan done! Took ", duration, ". Output file: ", outputPath)

	// TODO: Get rid of bash and do this using Go

	if isFolderOut {
		SaveInFolder(tempFolder, outputPath)
	} else {
		utils.RunCommand("cat "+filepath.Join(tempFolder, "chunk-out-*")+" > "+outputPath, true)
	}

	if chunksFolder == "" {
		//utils.RunCommand("rm -rf " + filepath.Join(tempFolder, "chunk-out-*"))
		os.RemoveAll(tempFolder)
	}
	return nil
}

func StartSingle(fleetName, command string, inputFile string, inputDestination string, outputPath, token string, port int, username, password string, provider controller.Provider) error {
	start := time.Now()

	utils.Log.Info("Scan started!")

	fleet := controller.GetFleet(fleetName, token, provider)
	if len(fleet) < 1 {
		return fmt.Errorf("No fleet found")
	}

	ip := fleet[0].IP

	if inputFile != "" {
		err := scp.NewSCP(sshutils.GetConnection(ip, port, username, password).Client).SendFile(inputFile, inputDestination)
		if err != nil {
			return err
		}
	}

	boxOutputFile := "/tmp/fleex-" + fleetName

	// Replace labels and craft final command
	finalCommand := command
	finalCommand = strings.ReplaceAll(finalCommand, "{{OUTPUT}}", boxOutputFile)
	finalCommand = strings.ReplaceAll(finalCommand, "{{INPUT}}", inputDestination)

	sshutils.RunCommand(finalCommand, ip, port, username, password)

	// Now download the output file
	SendSCP(boxOutputFile, outputPath, ip, port, username, password)

	// Remove input chunk file from remote box to save space
	sshutils.RunCommand("sudo rm -rf "+boxOutputFile, ip, port, username, password)

	// Scan done, process results
	duration := time.Since(start)
	utils.Log.Info("Scan done! Took ", duration, ". Output file: ", outputPath)

	return nil
}

func SendSCP(source string, destination string, IP string, PORT int, username string, password string) (bool, error) {
	err := scp.NewSCP(sshutils.GetConnection(IP, PORT, username, password).Client).ReceiveFile(source, destination)
	if err != nil {
		fmt.Printf("Error receiving single file: %s. Trying directory\n", err)
		os.Remove(destination)
		err := scp.NewSCP(sshutils.GetConnection(IP, PORT, username, password).Client).ReceiveDir(source, destination, nil)
		if err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

func SaveInFolder(inputPath string, outputPath string) {
	utils.MakeFolder(outputPath)

	filepath.Walk(inputPath,
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() {
				if !strings.Contains(info.Name(), "chunk-") {
					utils.RunCommand("cp "+path+" "+outputPath, true)
				}
			}
			return nil
		})
}

func IsDirectory(path string) (bool, error) {
	fileInfo, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	return fileInfo.IsDir(), err
}
