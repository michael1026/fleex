package services

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"

	"github.com/linode/linodego"
	"github.com/michael1026/fleex/pkg/provider"
	"github.com/michael1026/fleex/pkg/sshutils"
	"github.com/michael1026/fleex/pkg/utils"
	"github.com/spf13/viper"
)

type LinodeService struct {
	Client linodego.Client
}

func (l LinodeService) SpawnFleet(fleetName string, fleetCount int, image string, region string, size string, sshFingerprint string, tags []string) error {
	existingFleet, _ := l.GetFleet(fleetName)

	threads := 10
	fleet := make(chan string, threads)
	processGroup := new(sync.WaitGroup)
	processGroup.Add(threads)

	for i := 0; i < threads; i++ {
		go func() error {
			for {
				box := <-fleet

				if box == "" {
					break
				}

				utils.Log.Info("Spawning box ", box)
				err := l.spawnBox(box, image, region, size)
				if err != nil {
					return err
				}
			}
			processGroup.Done()
			return nil
		}()
	}

	for i := 0; i < fleetCount; i++ {
		fleet <- fleetName + "-" + strconv.Itoa(i+1+len(existingFleet))
	}

	close(fleet)
	processGroup.Wait()
	return nil
}

func (l LinodeService) GetFleet(fleetName string) (fleet []provider.Box, err error) {
	boxes, err := l.GetBoxes()
	if err != nil {
		return []provider.Box{}, err
	}

	for _, box := range boxes {
		if strings.HasPrefix(box.Label, fleetName) {
			fleet = append(fleet, box)
		}
	}
	return fleet, nil
}

func (l LinodeService) GetBox(boxName string) (provider.Box, error) {
	boxes, err := l.GetBoxes()
	if err != nil {
		return provider.Box{}, err
	}

	for _, box := range boxes {
		if box.Label == boxName {
			return box, err
		}
	}
	return provider.Box{}, provider.ErrBoxNotFound
}

func (l LinodeService) GetBoxes() (boxes []provider.Box, err error) {
	linodes, err := l.Client.ListInstances(context.Background(), nil)
	if err != nil {
		return []provider.Box{}, err
	}

	for _, linode := range linodes {
		linodeID := strconv.Itoa(linode.ID)
		boxes = append(boxes, provider.Box{
			ID:     linodeID,
			Label:  linode.Label,
			Group:  linode.Group,
			Status: string(linode.Status),
			IP:     linode.IPv4[0].String(),
		})
	}
	return boxes, nil
}

func (l LinodeService) getImages() (images []provider.Image, err error) {
	linodeImages, err := l.Client.ListImages(context.Background(), nil)

	if err != nil {
		return []provider.Image{}, err
	}

	for _, image := range linodeImages {
		// Only list custom images
		if strings.HasPrefix(image.ID, "private") {
			images = append(images, provider.Image{
				ID:      image.ID,
				Label:   image.Label,
				Created: image.Created.String(),
				Size:    image.Size,
				Vendor:  image.Vendor,
			})
		}
	}
	return images, nil
}

func (l LinodeService) ListBoxes() {
	boxes, _ := l.GetBoxes()
	for _, linode := range boxes {
		fmt.Printf("%-10v %-16v %-10v %-20v %-15v\n", linode.ID, linode.Label, linode.Group, linode.Status, linode.IP)
	}
}

func (l LinodeService) ListImages() error {
	images, err := l.getImages()
	if err != nil {
		return err
	}
	for _, image := range images {
		fmt.Printf("%-18v %-48v %-6v %-29v %-15v\n", image.ID, image.Label, image.Size, image.Created, image.Vendor)
	}
	return nil
}

func (l LinodeService) RemoveImages(name string) error {
	images, err := l.getImages()
	if err != nil {
		return err
	}
	for _, image := range images {
		if image.Label == name {
			err := l.Client.DeleteImage(context.Background(), image.ID)
			if err != nil {
				return err
			}
			fmt.Println("Successfully removed:", name)
		}
		fmt.Printf("%-18v %-48v %-6v %-29v %-15v\n", image.ID, image.Label, image.Size, image.Created, image.Vendor)
	}
	return errors.New("Image not found")
}

func (l LinodeService) spawnBox(name string, image string, region string, size string) error {
	for {
		linPasswd := viper.GetString("linode.password")

		swapSize := 512
		booted := true
		instance, err := l.Client.CreateInstance(context.Background(), linodego.InstanceCreateOptions{
			SwapSize:       &swapSize,
			Image:          image,
			RootPass:       linPasswd,
			Type:           size,
			Region:         region,
			AuthorizedKeys: []string{sshutils.GetLocalPublicSSHKey()},
			Booted:         &booted,
			Label:          name,
		})

		if err != nil {
			if strings.Contains(err.Error(), "Please try again") {
				continue
			}
			return err
		}
		// Sometimes a few instances do not boot automatically
		l.Client.BootInstance(context.Background(), instance.ID, 0)
		break
	}
	return nil
}

func (l LinodeService) DeleteFleet(name string) error {
	// TODO manage error
	boxes, err := l.GetBoxes()
	if err != nil {
		return err
	}
	for _, box := range boxes {
		if box.Label == name {
			// We only have to delete a single box
			err := l.DeleteBoxByID(box.ID)
			if err != nil {
				return err
			}
			return nil
		}
	}

	// Otherwise, we got a fleet to delete
	fleetSize := l.CountFleet(name, boxes)

	fleet := make(chan *provider.Box, fleetSize)
	processGroup := new(sync.WaitGroup)
	processGroup.Add(fleetSize)

	for i := 0; i < fleetSize; i++ {
		go func() error {
			for {
				box := <-fleet

				if box == nil {
					break
				}
				err := l.DeleteBoxByID(box.ID)
				if err != nil {
					return err
				}
			}
			processGroup.Done()
			return nil
		}()
	}

	for i := range boxes {
		if strings.HasPrefix(boxes[i].Label, name) {
			fleet <- &boxes[i]
		}
	}

	close(fleet)
	processGroup.Wait()
	return nil
}

func (l LinodeService) DeleteBoxByID(id string) error {
	linodeID, _ := strconv.Atoi(id)
	err := l.Client.DeleteInstance(context.Background(), linodeID)
	if err != nil {
		return err
	}
	return nil
}

func (l LinodeService) DeleteBoxByLabel(label string) error {
	boxes, err := l.GetBoxes()
	if err != nil {
		return err
	}
	for _, linode := range boxes {
		if linode.Label == label && linode.Label != "BugBountyUbuntu" {
			err := l.DeleteBoxByID(linode.ID)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (l LinodeService) CountFleet(fleetName string, boxes []provider.Box) (count int) {
	for _, box := range boxes {
		if strings.HasPrefix(box.Label, fleetName) {
			count++
		}
	}
	return count
}

func (l LinodeService) RunCommand(name, command string, port int, username, password string) error {
	// TODO manage error
	boxes, err := l.GetBoxes()
	if err != nil {
		return err
	}
	for _, box := range boxes {
		if box.Label == name {
			fmt.Println("AAA")
			// It's a single box
			sshutils.RunCommandWithPassword(command, box.IP, port, username, password)
			return nil
		}
	}

	// Otherwise, send command to a fleet
	fleetSize := l.CountFleet(name, boxes)

	fleet := make(chan *provider.Box, fleetSize)
	processGroup := new(sync.WaitGroup)
	processGroup.Add(fleetSize)

	for i := 0; i < fleetSize; i++ {
		go func() {
			for {
				box := <-fleet

				if box == nil {
					break
				}
				sshutils.RunCommandWithPassword(command, box.IP, port, username, password)
			}
			processGroup.Done()
		}()
	}

	for i := range boxes {
		if strings.HasPrefix(boxes[i].Label, name) {
			fleet <- &boxes[i]
		}
	}

	close(fleet)
	processGroup.Wait()
	return nil
}

// ─── IMAGE CREATION ─────────────────────────────────────────────────────────────

func (l LinodeService) CreateImage(diskID int, label string) error {
	linodeID := l.getDiskID(diskID)
	_, err := l.Client.CreateImage(context.Background(), linodego.ImageCreateOptions{
		DiskID:      linodeID,
		Description: "Fleex build image",
		Label:       label,
	})
	if err != nil {
		return err
	}
	return nil
}

func (l LinodeService) getDiskID(linodeID int) int {
	disk, err := l.Client.ListInstanceDisks(context.Background(), linodeID, nil)
	if err != nil {
		log.Fatal(err)
	}
	return disk[0].ID
}
