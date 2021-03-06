package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Microsoft/hcsshim/internal/hcs"
	"github.com/Microsoft/hcsshim/internal/uvm"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli"
)

const (
	kernelArgsArgName           = "kernel-args"
	rootFSTypeArgName           = "root-fs-type"
	vpMemMaxCountArgName        = "vpmem-max-count"
	vpMemMaxSizeArgName         = "vpmem-max-size"
	cpusArgName                 = "cpus"
	memoryArgName               = "memory"
	allowOvercommitArgName      = "allow-overcommit"
	enableDeferredCommitArgName = "enable-deferred-commit"
	measureArgName              = "measure"
	parallelArgName             = "parallel"
	countArgName                = "count"
	kernelDirectArgName         = "kernel-direct"
	execCommandLineArgName      = "exec"
	forwardStdoutArgName        = "fwd-stdout"
	forwardStderrArgName        = "fwd-stderr"
	debugArgName                = "debug"
	outputHandlingArgName       = "output-handling"
)

func main() {
	app := cli.NewApp()
	app.Name = "uvmboot"
	app.Usage = "Boot a utility VM"

	app.Flags = []cli.Flag{
		cli.Uint64Flag{
			Name:  cpusArgName,
			Usage: "Number of CPUs on the UVM. Uses hcsshim default if not specified",
		},
		cli.UintFlag{
			Name:  memoryArgName,
			Usage: "Amount of memory on the UVM, in MB. Uses hcsshim default if not specified",
		},
		cli.BoolFlag{
			Name:  measureArgName,
			Usage: "Measure wall clock time of the UVM run",
		},
		cli.IntFlag{
			Name:  parallelArgName,
			Value: 1,
			Usage: "Number of UVMs to boot in parallel",
		},
		cli.IntFlag{
			Name:  countArgName,
			Value: 1,
			Usage: "Total number of UVMs to run",
		},
		cli.BoolFlag{
			Name:  allowOvercommitArgName,
			Usage: "Allow memory overcommit on the UVM",
		},
		cli.BoolFlag{
			Name:  enableDeferredCommitArgName,
			Usage: "Enable deferred commit on the UVM",
		},
		cli.BoolFlag{
			Name:  debugArgName,
			Usage: "Enable debug level logging in HCSShim",
		},
	}

	app.Commands = []cli.Command{
		{
			Name:  "lcow",
			Usage: "Boot an LCOW UVM",
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  kernelArgsArgName,
					Value: "",
					Usage: "Additional arguments to pass to the kernel",
				},
				cli.StringFlag{
					Name:  rootFSTypeArgName,
					Usage: "Either 'initrd' or 'vhd'. Uses hcsshim default if not specified",
				},
				cli.UintFlag{
					Name:  vpMemMaxCountArgName,
					Usage: "Number of VPMem devices on the UVM. Uses hcsshim default if not specified",
				},
				cli.Uint64Flag{
					Name:  vpMemMaxSizeArgName,
					Usage: "Size of each VPMem device, in MB. Uses hcsshim default if not specified",
				},
				cli.BoolFlag{
					Name:  kernelDirectArgName,
					Usage: "Use kernel direct booting for UVM",
				},
				cli.StringFlag{
					Name:  execCommandLineArgName,
					Usage: "Command to execute in the UVM.",
				},
				cli.BoolFlag{
					Name:  forwardStdoutArgName,
					Usage: "Whether stdout from the process in the UVM should be forwarded",
				},
				cli.BoolFlag{
					Name:  forwardStderrArgName,
					Usage: "Whether stderr from the process in the UVM should be forwarded",
				},
				cli.StringFlag{
					Name:  outputHandlingArgName,
					Usage: "Controls how output from UVM is handled. Use 'stdout' to print all output to stdout",
				},
			},
			Action: func(c *cli.Context) error {
				if c.GlobalBool("debug") {
					logrus.SetLevel(logrus.DebugLevel)
				}

				parallelCount := c.GlobalInt(parallelArgName)

				var wg sync.WaitGroup
				wg.Add(parallelCount)

				workChan := make(chan int)

				runFunc := func(workChan <-chan int) {
					for {
						i, ok := <-workChan

						if !ok {
							wg.Done()
							return
						}

						id := fmt.Sprintf("uvmboot-%d", i)

						options := uvm.OptionsLCOW{
							Options: &uvm.Options{
								ID: id,
							},
						}

						{
							val := false
							options.UseGuestConnection = &val
						}

						if c.GlobalIsSet(cpusArgName) {
							options.ProcessorCount = int32(c.GlobalUint64(cpusArgName))
						}
						if c.GlobalIsSet(memoryArgName) {
							options.MemorySizeInMB = int32(c.GlobalUint64(memoryArgName))
						}
						if c.GlobalIsSet(allowOvercommitArgName) {
							val := c.GlobalBool(allowOvercommitArgName)
							options.AllowOvercommit = &val
						}
						if c.GlobalIsSet(enableDeferredCommitArgName) {
							val := c.GlobalBool(enableDeferredCommitArgName)
							options.EnableDeferredCommit = &val
						}

						if c.IsSet(kernelDirectArgName) {
							options.KernelDirect = c.Bool(kernelDirectArgName)
						}
						if c.IsSet(rootFSTypeArgName) {
							switch strings.ToLower(c.String(rootFSTypeArgName)) {
							case "initrd":
								val := uvm.PreferredRootFSTypeInitRd
								options.PreferredRootFSType = &val
							case "vhd":
								val := uvm.PreferredRootFSTypeVHD
								options.PreferredRootFSType = &val
							default:
								logrus.Fatalf("Unrecognized value '%s' for option %s", c.String(rootFSTypeArgName), rootFSTypeArgName)
							}
						}
						if c.IsSet(kernelArgsArgName) {
							options.KernelBootOptions = c.String(kernelArgsArgName)
						}
						if c.IsSet(vpMemMaxCountArgName) {
							val := uint32(c.Uint(vpMemMaxCountArgName))
							options.VPMemDeviceCount = &val
						}
						if c.IsSet(vpMemMaxSizeArgName) {
							val := c.Uint64(vpMemMaxSizeArgName) * 1024 * 1024 // convert from MB to bytes
							options.VPMemSizeBytes = &val
						}
						if c.IsSet(execCommandLineArgName) {
							options.ExecCommandLine = c.String(execCommandLineArgName)
						}
						if c.IsSet(forwardStdoutArgName) {
							val := c.Bool(forwardStdoutArgName)
							options.ForwardStdout = &val
						}
						if c.IsSet(forwardStderrArgName) {
							val := c.Bool(forwardStderrArgName)
							options.ForwardStderr = &val
						}
						if c.IsSet(outputHandlingArgName) {
							switch strings.ToLower(c.String(outputHandlingArgName)) {
							case "stdout":
								val := uvm.OutputHandler(func(r io.Reader) { io.Copy(os.Stdout, r) })
								options.OutputHandler = &val
							default:
								logrus.Fatalf("Unrecognized value '%s' for option %s", c.String(outputHandlingArgName), outputHandlingArgName)
							}
						}

						if err := run(&options); err != nil {
							logrus.WithField("uvm-id", id).Error(err)
						}
					}
				}

				for i := 0; i < parallelCount; i++ {
					go runFunc(workChan)
				}

				start := time.Now()

				for i := 0; i < c.GlobalInt(countArgName); i++ {
					workChan <- i
				}

				close(workChan)

				wg.Wait()

				if c.GlobalBool(measureArgName) {
					fmt.Println("Elapsed time:", time.Since(start))
				}

				return nil
			},
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		logrus.Fatal(err)
	}
}

func run(options *uvm.OptionsLCOW) error {
	uvm, err := uvm.CreateLCOW(options)
	if err != nil {
		return err
	}
	defer uvm.Close()

	if err := uvm.Start(); err != nil {
		return err
	}

	if err := uvm.WaitExpectedError(hcs.ErrVmcomputeUnexpectedExit); err != nil {
		return err
	}

	return nil
}
