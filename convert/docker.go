package convert

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types/blkiodev"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/go-connections/nat"
	v3 "github.com/rancher/go-rancher/v3"
)

type Reference struct {
	VolumeFromContainers map[string]v3.Container
	DockerRoot           string
}

// RancherToDockerConfigs take a v3.Container and a list of metadata referenced by itself, returns docker.Config and docker.HostConfig
func RancherToDockerConfigs(containerSpec v3.Container, reference Reference) (container.Config, container.HostConfig) {
	config := &container.Config{}
	hostConfig := &container.HostConfig{}

	//config
	config.OpenStdin = containerSpec.StdinOpen
	config.Labels = ToMapString(containerSpec.Labels)
	config.Cmd = containerSpec.Command
	config.Env = convertEnv(containerSpec.Environment)
	config.WorkingDir = containerSpec.WorkingDir
	config.Entrypoint = containerSpec.EntryPoint
	config.Tty = containerSpec.Tty
	config.Domainname = containerSpec.DomainName
	config.StopSignal = containerSpec.StopSignal
	config.User = containerSpec.User
	config.Hostname = containerSpec.Hostname

	//hostConfig
	hostConfig.PublishAllPorts = containerSpec.PublishAllPorts
	hostConfig.DNS = containerSpec.Dns
	hostConfig.DNSSearch = containerSpec.DnsSearch
	hostConfig.DNSOptions = containerSpec.DnsOpt
	hostConfig.Binds = hostConfig.Binds
	hostConfig.CapAdd = containerSpec.CapAdd
	hostConfig.CapDrop = containerSpec.CapDrop
	hostConfig.CpusetCpus = containerSpec.CpuSet
	hostConfig.BlkioWeight = uint16(containerSpec.BlkioWeight)
	hostConfig.CgroupParent = containerSpec.CgroupParent
	hostConfig.CPUPeriod = containerSpec.CpuPeriod
	hostConfig.CPUQuota = containerSpec.CpuQuota
	hostConfig.CpusetMems = containerSpec.CpuSetMems
	hostConfig.GroupAdd = containerSpec.GroupAdd
	hostConfig.KernelMemory = containerSpec.KernelMemory
	hostConfig.MemorySwap = containerSpec.MemorySwap
	hostConfig.Memory = containerSpec.Memory
	hostConfig.MemorySwappiness = &containerSpec.MemorySwappiness
	hostConfig.OomKillDisable = &containerSpec.OomKillDisable
	hostConfig.OomScoreAdj = int(containerSpec.OomScoreAdj)
	hostConfig.ShmSize = containerSpec.ShmSize
	hostConfig.Tmpfs = ToMapString(containerSpec.Tmpfs)
	hostConfig.Ulimits = ConvertUlimits(containerSpec.Ulimits)
	hostConfig.UTSMode = container.UTSMode(containerSpec.Uts)
	hostConfig.IpcMode = container.IpcMode(containerSpec.IpcMode)
	hostConfig.Sysctls = ToMapString(containerSpec.Sysctls)
	hostConfig.StorageOpt = ToMapString(containerSpec.StorageOpt)
	hostConfig.PidsLimit = containerSpec.PidsLimit
	hostConfig.DiskQuota = containerSpec.DiskQuota
	hostConfig.CgroupParent = containerSpec.CgroupParent
	hostConfig.UsernsMode = container.UsernsMode(containerSpec.UsernsMode)
	hostConfig.ExtraHosts = containerSpec.ExtraHosts
	hostConfig.PidMode = container.PidMode(containerSpec.PidMode)

	//special convert
	setupPorts(config, hostConfig, containerSpec)
	setupVolume(config, hostConfig, containerSpec, reference)
	setupDevice(hostConfig, containerSpec)
	setupLogConfig(hostConfig, containerSpec)
	setupDeviceOptions(hostConfig, containerSpec)
	setupHeathConfig(config, containerSpec)

	return *config, *hostConfig
}

func convertEnv(envs map[string]interface{}) []string {
	r := []string{}
	for k, v := range envs {
		r = append(r, fmt.Sprintf("%v:%v", k, v))
	}
	return r
}

func setupPorts(config *container.Config, hostConfig *container.HostConfig, containerSpec v3.Container) {
	//ports := []types.Port{}
	exposedPorts := map[nat.Port]struct{}{}
	bindings := nat.PortMap{}
	for _, endpoint := range containerSpec.PublicEndpoints {
		if endpoint.PrivatePort != 0 {
			bind := nat.Port(fmt.Sprintf("%v/%v", endpoint.PrivatePort, endpoint.Protocol))
			bindAddr := endpoint.BindIpAddress
			if _, ok := bindings[bind]; !ok {
				bindings[bind] = []nat.PortBinding{
					{
						HostIP:   bindAddr,
						HostPort: strconv.Itoa(int(endpoint.PublicPort)),
					},
				}
			} else {
				bindings[bind] = append(bindings[bind], nat.PortBinding{
					HostIP:   bindAddr,
					HostPort: strconv.Itoa(int(endpoint.PublicPort)),
				})
			}
			exposedPorts[bind] = struct{}{}
		}

	}

	config.ExposedPorts = exposedPorts

	if len(bindings) > 0 {
		hostConfig.PortBindings = bindings
	}
}

func setupVolume(config *container.Config, hostConfig *container.HostConfig, containerSpec v3.Container, reference Reference) {
	config.Volumes = map[string]struct{}{}
	volumesMap := map[string]struct{}{}
	binds := []string{}
	for _, dataVolume := range containerSpec.DataVolumes {
		parts := strings.SplitN(dataVolume, ":", 3)
		if len(parts) == 1 {
			config.Volumes[parts[0]] = struct{}{}
		} else if len(parts) > 1 {
			volumesMap[parts[1]] = struct{}{}
			mode := ""
			if len(parts) == 3 {
				mode = parts[2]
			} else {
				mode = "rw"
			}

			// Redirect /var/lib/docker:/var/lib/docker to where Docker root really is
			if parts[0] == "/var/lib/docker" && parts[1] == "/var/lib/docker" {
				if reference.DockerRoot != "/var/lib/docker" {
					volumesMap[reference.DockerRoot] = struct{}{}
					binds = append(binds, fmt.Sprintf("%s:%s:%s", reference.DockerRoot, parts[1], mode))
					binds = append(binds, fmt.Sprintf("%s:%s:%s", reference.DockerRoot, reference.DockerRoot, mode))
					continue
				}
			}
			bind := fmt.Sprintf("%s:%s:%s", parts[0], parts[1], mode)
			binds = append(binds, bind)
		}
	}
	config.Volumes = volumesMap
	hostConfig.Binds = append(hostConfig.Binds, binds...)

	volumeFroms := []string{}
	if containerSpec.DataVolumesFrom != nil {
		for _, volumeFrom := range containerSpec.DataVolumesFrom {
			if reference.VolumeFromContainers[volumeFrom].ExternalId != "" {
				volumeFroms = append(volumeFroms, reference.VolumeFromContainers[volumeFrom].ExternalId)
			}
		}
		if len(volumeFroms) > 0 {
			hostConfig.VolumesFrom = volumeFroms
		}
	}
}

func setupDevice(hostConfig *container.HostConfig, containerSpec v3.Container) {
	deviceMappings := []container.DeviceMapping{}
	devices := containerSpec.Devices
	for _, device := range devices {
		parts := strings.Split(device, ":")
		permission := "rwm"
		if len(parts) == 3 {
			permission = parts[2]
		}
		deviceMappings = append(deviceMappings,
			container.DeviceMapping{
				PathOnHost:        parts[0],
				PathInContainer:   parts[1],
				CgroupPermissions: permission,
			})
	}

	hostConfig.Devices = deviceMappings
}

func setupLogConfig(hostConfig *container.HostConfig, containerSpec v3.Container) {
	if containerSpec.LogConfig != nil {
		hostConfig.LogConfig.Type = containerSpec.LogConfig.Driver
		hostConfig.LogConfig.Config = ToMapString(containerSpec.LogConfig.Config)
	}
}

type deviceOptions struct {
	Weight    uint16
	ReadIops  uint64
	WriteIops uint64
	ReadBps   uint64
	WriteBps  uint64
}

func setupDeviceOptions(hostConfig *container.HostConfig, spec v3.Container) error {
	devOptions := spec.BlkioDeviceOptions

	blkioWeightDevice := []*blkiodev.WeightDevice{}
	blkioDeviceReadIOps := []*blkiodev.ThrottleDevice{}
	blkioDeviceWriteBps := []*blkiodev.ThrottleDevice{}
	blkioDeviceReadBps := []*blkiodev.ThrottleDevice{}
	blkioDeviceWriteIOps := []*blkiodev.ThrottleDevice{}

	for dev, value := range devOptions {
		option := deviceOptions{}
		if err := Unmarshalling(value, &option); err != nil {
			continue
		}
		if option.Weight != 0 {
			blkioWeightDevice = append(blkioWeightDevice, &blkiodev.WeightDevice{
				Path:   dev,
				Weight: option.Weight,
			})
		}
		if option.ReadIops != 0 {
			blkioDeviceReadIOps = append(blkioDeviceReadIOps, &blkiodev.ThrottleDevice{
				Path: dev,
				Rate: option.ReadIops,
			})
		}
		if option.WriteIops != 0 {
			blkioDeviceWriteIOps = append(blkioDeviceWriteIOps, &blkiodev.ThrottleDevice{
				Path: dev,
				Rate: option.WriteIops,
			})
		}
		if option.ReadBps != 0 {
			blkioDeviceReadBps = append(blkioDeviceReadBps, &blkiodev.ThrottleDevice{
				Path: dev,
				Rate: option.ReadBps,
			})
		}
		if option.WriteBps != 0 {
			blkioDeviceWriteBps = append(blkioDeviceWriteBps, &blkiodev.ThrottleDevice{
				Path: dev,
				Rate: option.WriteBps,
			})
		}
	}
	if len(blkioWeightDevice) > 0 {
		hostConfig.BlkioWeightDevice = blkioWeightDevice
	}
	if len(blkioDeviceReadIOps) > 0 {
		hostConfig.BlkioDeviceReadIOps = blkioDeviceReadIOps
	}
	if len(blkioDeviceWriteIOps) > 0 {
		hostConfig.BlkioDeviceWriteIOps = blkioDeviceWriteIOps
	}
	if len(blkioDeviceReadBps) > 0 {
		hostConfig.BlkioDeviceReadBps = blkioDeviceReadBps
	}
	if len(blkioDeviceWriteBps) > 0 {
		hostConfig.BlkioDeviceWriteBps = blkioDeviceWriteBps
	}
	return nil
}

func setupHeathConfig(config *container.Config, spec v3.Container) {
	healthConfig := &container.HealthConfig{}
	healthConfig.Test = spec.HealthCmd
	healthConfig.Interval = time.Duration(spec.HealthInterval) * time.Second
	healthConfig.Retries = int(spec.HealthRetries)
	healthConfig.Timeout = time.Duration(spec.HealthTimeout) * time.Second
	config.Healthcheck = healthConfig
}
