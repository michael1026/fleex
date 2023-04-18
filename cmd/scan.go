package cmd

import (
	"io/ioutil"

	"github.com/michael1026/fleex/pkg/controller"
	"github.com/michael1026/fleex/pkg/scan"
	"github.com/michael1026/fleex/pkg/utils"
	"github.com/mitchellh/go-homedir"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v2"
)

type Module struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Author      string `yaml:"author"`
	Command     string `yaml:"command"`
}

// scanCmd represents the scan command
var scanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Send a command to a fleet, but also with files upload & chunks splitting",
	Run: func(cmd *cobra.Command, args []string) {
		var token string

		proxy, _ := rootCmd.PersistentFlags().GetString("proxy")
		utils.SetProxy(proxy)

		providerFlag, _ := cmd.Flags().GetString("provider")
		commandFlag, _ := cmd.Flags().GetString("command")
		deleteFlag, _ := cmd.Flags().GetBool("delete")
		fleetNameFlag, _ := cmd.Flags().GetString("name")
		inputFlag, _ := cmd.Flags().GetString("input")
		output, _ := cmd.Flags().GetString("output")
		moduleFlag, _ := cmd.Flags().GetString("module")

		chunksFolder, _ := cmd.Flags().GetString("chunks-folder")
		if providerFlag != "" {
			viper.Set("provider", providerFlag)
		}
		provider := controller.GetProvider(viper.GetString("provider"))
		providerFlag = viper.GetString("provider")

		port, _ := cmd.Flags().GetInt("port")
		username, _ := cmd.Flags().GetString("username")
		password, _ := cmd.Flags().GetString("password")
		if port != -1 {
			viper.Set(providerFlag+".port", port)
		}
		if username != "" {
			viper.Set(providerFlag+".username", username)
		}
		if password != "" {
			viper.Set(providerFlag+".password", password)
		}

		switch provider {
		case controller.PROVIDER_LINODE:
			token = viper.GetString("linode.token")
			port = viper.GetInt("linode.port")
			username = viper.GetString("linode.username")
			password = viper.GetString("linode.password")
		case controller.PROVIDER_DIGITALOCEAN:
			token = viper.GetString("digitalocean.token")
			port = viper.GetInt("digitalocean.port")
			username = viper.GetString("digitalocean.username")
			password = viper.GetString("digitalocean.password")
		case controller.PROVIDER_VULTR:
			token = viper.GetString("vultr.token")
			port = viper.GetInt("vultr.port")
			username = viper.GetString("vultr.username")
			password = viper.GetString("vultr.password")
		}

		var module Module

		if moduleFlag != "" {
			selectedModule := module.getModule(moduleFlag)
			commandFlag = selectedModule.Command
			utils.Log.Info(selectedModule.Name, ": ", selectedModule.Description)
			utils.Log.Info("Created by: ", selectedModule.Author)
		}

		if commandFlag == "" {
			utils.Log.Fatal("Command not found, insert a command or module")
		}

		scan.Start(fleetNameFlag, commandFlag, deleteFlag, inputFlag, output, chunksFolder, token, port, username, password, provider)

	},
}

func init() {
	rootCmd.AddCommand(scanCmd)
	scanCmd.Flags().StringP("name", "n", "pwn", "Fleet name")
	scanCmd.Flags().StringP("command", "c", "", "Command to send. Supports {{INPUT}} and {{OUTPUT}}")
	scanCmd.Flags().StringP("input", "i", "", "Input file")
	scanCmd.Flags().StringP("output", "o", "", "Output file path. Made from concatenating all output chunks from all boxes")
	scanCmd.Flags().StringP("chunks-folder", "", "", "Output folder containing output chunks. If empty it will use /tmp/<unix_timestamp>")
	scanCmd.Flags().StringP("provider", "p", "", "VPS provider (Supported: linode, digitalocean, vultr)")
	scanCmd.Flags().IntP("port", "", -1, "SSH port")
	scanCmd.Flags().StringP("username", "U", "", "SSH username")
	scanCmd.Flags().StringP("password", "P", "", "SSH password")
	scanCmd.Flags().BoolP("delete", "d", false, "Delete boxes as soon as they finish their job")
	scanCmd.Flags().StringP("module", "m", "", "Scan modules")

	scanCmd.MarkFlagRequired("output")
	// scanCmd.MarkFlagRequired("command")
}

func (m *Module) getModule(modulename string) *Module {
	home, _ := homedir.Dir()
	yamlFile, err := ioutil.ReadFile(home + "/fleex/modules/" + modulename + ".yaml")
	if err != nil {
		utils.Log.Fatal("yamlFile.Get:", err)
	}
	err = yaml.Unmarshal(yamlFile, m)
	if err != nil {
		utils.Log.Fatal("Unmarshal:", err)
	}
	return m
}
