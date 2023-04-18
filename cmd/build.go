package cmd

import (
	"fmt"
	"io/ioutil"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/hnakamur/go-scp"
	"github.com/michael1026/fleex/pkg/controller"
	"github.com/michael1026/fleex/pkg/sshutils"
	"github.com/michael1026/fleex/pkg/utils"
	"github.com/mitchellh/go-homedir"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v2"
)

type BuildConfig struct {
	//Name   string
	Config struct {
		Source      string `yaml:"source"`
		Destination string `yaml:"destination"`
	}

	Commands []string
}

// buildCmd represents the build command
var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Build an image with all the tools you need. Run this the first time only (for each provider).",
	Long:  "Build image",
	Run: func(cmd *cobra.Command, args []string) {
		var token, region, size, sshFingerprint, boxIP, image string
		var boxID string

		proxy, _ := rootCmd.PersistentFlags().GetString("proxy")
		utils.SetProxy(proxy)

		timeNow := strconv.FormatInt(time.Now().Unix(), 10)
		home, _ := homedir.Dir()
		fleetName := "fleex-" + timeNow

		publicSSH := viper.GetString("public-ssh-file")
		tags := []string{"snapshot"}

		providerFlag, _ := cmd.Flags().GetString("provider")
		regionFlag, _ := cmd.Flags().GetString("region")
		sizeFlag, _ := cmd.Flags().GetString("size")
		fileFlag, _ := cmd.Flags().GetString("file")
		noDeleteFlag, _ := cmd.Flags().GetBool("no-delete")
		debugFlag, _ := cmd.Flags().GetBool("debug")

		if providerFlag != "" {
			viper.Set("provider", providerFlag)
		}
		provider := controller.GetProvider(viper.GetString("provider"))
		providerFlag = viper.GetString("provider")

		if regionFlag != "" {
			viper.Set(providerFlag+".region", regionFlag)
		}
		if sizeFlag != "" {
			viper.Set(providerFlag+".size", sizeFlag)
		}

		switch provider {
		case controller.PROVIDER_LINODE:
			token = viper.GetString("linode.token")
			region = viper.GetString("linode.region")
			size = viper.GetString("linode.size")
			image = "linode/ubuntu20.04"
		case controller.PROVIDER_DIGITALOCEAN:
			token = viper.GetString("digitalocean.token")
			region = viper.GetString("digitalocean.region")
			size = viper.GetString("digitalocean.size")
			sshFingerprint = sshutils.SSHFingerprintGen(publicSSH)
			image = "ubuntu-20-04-x64"
		case controller.PROVIDER_VULTR:
			token = viper.GetString("vultr.token")
			region = viper.GetString("vultr.region")
			size = viper.GetString("vultr.size")
			sshFingerprint = sshutils.SSHFingerprintGen(publicSSH)
			image = "270"
		}

		// Check for authorization_keys
		pubSSH := viper.GetString("public-ssh-file")
		if pubSSH == "" {
			utils.Log.Fatal("You need to create a Key Pair for SSH")
		}

		utils.Copy(home+"/.ssh/"+pubSSH, home+"/fleex/configs/authorized_keys")

		if provider == controller.PROVIDER_LINODE {
			packerVars := "-var 'TOKEN=" + token + "'"
			packerVars += " -var 'IMAGE=" + image + "'"
			packerVars += " -var 'SIZE=" + size + "'"
			packerVars += " -var 'REGION=" + region + "'"
			utils.RunCommand("packer build "+packerVars+" "+fileFlag, debugFlag)
		} else {
			c, err := readConf(fileFlag)
			if err != nil {
				log.Fatal(err)
			}

			controller.SpawnFleet(fleetName, 1, image, region, size, sshFingerprint, tags, token, false, provider, true)

			for {
				stillNotReady := false
				fleets := controller.GetFleet(fleetName+"-1", token, provider)
				if len(fleets) == 0 {
					stillNotReady = true
				}
				for _, box := range fleets {
					if box.Label == fleetName+"-1" {
						boxID = box.ID
						boxIP = box.IP
						break
					}
				}

				if stillNotReady {
					time.Sleep(3 * time.Second)
				} else {
					break
				}
			}

			if strings.ContainsAny("~", c.Config.Source) {
				c.Config.Source = strings.ReplaceAll(c.Config.Source, "~", home)
			}

			for {
				stillNotReady := false
				_, err := sshutils.GetConnectionBuild(boxIP, 22, "root", "1337superPass")
				if err != nil {
					stillNotReady = true
				}

				if stillNotReady {
					time.Sleep(5 * time.Second)
				} else {
					break
				}
			}

			err = scp.NewSCP(sshutils.GetConnection(boxIP, 22, "root", "1337superPass").Client).SendDir(c.Config.Source, c.Config.Destination, nil)
			if err != nil {
				log.Fatal(err)
			}

			if provider == controller.PROVIDER_DIGITALOCEAN {
				c.Commands = append(c.Commands, `/bin/su -l op -c "curl http://169.254.169.254/metadata/v1/user-data > /home/op/install.sh"`)
				c.Commands = append(c.Commands, `/bin/su -l op -c "chmod +x /home/op/install.sh"`)
				c.Commands = append(c.Commands, `/bin/su -l op -c "/home/op/install.sh"`)
			}

			for _, command := range c.Commands {
				controller.RunCommand(fleetName+"-1", command, token, 22, "root", "1337superPass", provider)
			}

			time.Sleep(8 * time.Second)
			controller.CreateImage(token, provider, boxID, "Fleex-build-"+timeNow)
			if !noDeleteFlag {
				time.Sleep(5 * time.Second)
				controller.DeleteFleet(fleetName+"-1", token, provider)
			}
			utils.Log.Info("\nImage done!")
		}
	},
}

func init() {
	home, _ := homedir.Dir()
	rootCmd.AddCommand(buildCmd)
	buildCmd.Flags().StringP("provider", "p", "", "Service provider (Supported: linode, digitalocean, vultr)")
	buildCmd.Flags().StringP("file", "f", home+"/fleex/build/common.yaml", "Build file")
	buildCmd.Flags().StringP("region", "R", "", "Region")
	buildCmd.Flags().StringP("size", "S", "", "Size")
	buildCmd.Flags().BoolP("no-delete", "", false, "Don't delete the box after image creation")
	buildCmd.Flags().BoolP("debug", "D", false, "Show build logs")

}

func readConf(filename string) (*BuildConfig, error) {
	buf, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	c := &BuildConfig{}
	err = yaml.Unmarshal(buf, c)
	if err != nil {
		return nil, fmt.Errorf("in file %q: %v", filename, err)
	}

	return c, nil
}
