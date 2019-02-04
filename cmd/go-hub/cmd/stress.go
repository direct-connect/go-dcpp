package cmd

import (
	"encoding/csv"
	"fmt"
	"log"
	"math/rand"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"

	"github.com/direct-connect/go-dcpp/nmdc/client"
)

var stressCmd = &cobra.Command{
	Use:   "stress",
	Short: "run the stress test against the hub",
}

func init() {
	fAddr := stressCmd.Flags().String("addr", "dchub://localhost:1411", "hub address to connect to")
	fNum := stressCmd.Flags().Int("n", 10000, "number of concurrent connections")
	fName := stressCmd.Flags().String("name", "bot_", "name prefix")
	fOut := stressCmd.Flags().String("out", "online.csv", "name for an output file")
	fMsg := stressCmd.Flags().Bool("msg", false, "send chat messages")
	fDur := stressCmd.Flags().Duration("dur", time.Second*30, "test duration")
	fAll := stressCmd.Flags().Bool("all", false, "do not disconnect, join from all routines")
	Root.AddCommand(stressCmd)
	stressCmd.RunE = func(cmd *cobra.Command, args []string) error {
		var wg sync.WaitGroup

		f, err := os.Create(*fOut)
		if err != nil {
			return err
		}
		defer f.Close()

		var (
			success   int32
			errors    int32
			connected int32
			max       int32
		)

		cw := csv.NewWriter(f)
		defer cw.Flush()

		sleep := func(done <-chan struct{}) bool {
			dt := time.Duration(rand.Int63n(int64(time.Second * 5)))
			t := time.NewTimer(dt)
			defer t.Stop()
			select {
			case <-t.C:
				return true
			case <-done:
				return false
			}
		}

		connect := func(done <-chan struct{}) bool {
			name := fmt.Sprintf(*fName+"%x", rand.Int())

			//start := time.Now()
			c, err := client.DialHub(*fAddr, &client.Config{
				Name: name,
			})
			//dt := time.Since(start)
			if err != nil {
				atomic.AddInt32(&errors, +1)
				log.Println("handshake failed:", err)
				return true
			}
			defer c.Close()
			atomic.AddInt32(&success, +1)
			cn := atomic.AddInt32(&connected, +1)
			defer atomic.AddInt32(&connected, -1)
			for old := atomic.LoadInt32(&max); old < cn && !atomic.CompareAndSwapInt32(&max, old, cn); old = atomic.LoadInt32(&max) {
			}
			if *fAll {
				<-done
				return false
			}

			for rand.Intn(10) < 5 {
				if *fMsg {
					_ = c.SendChatMsg(strconv.FormatUint(rand.Uint64(), 16))
				}
				if !sleep(done) {
					return false
				}
			}
			return true
		}

		done := make(chan struct{})

		for i := 0; i < *fNum; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if *fAll {
					connect(done)
					return
				}
				for connect(done) {
				}
			}()
		}
		const dt = time.Second / 2
		var (
			oldSuccess int32
			oldErrors  int32
		)
		for i := 0; i < int(*fDur/dt); i++ {
			time.Sleep(dt)
			sn, en := atomic.LoadInt32(&success), atomic.LoadInt32(&errors)
			dsn, den := sn-oldSuccess, en-oldErrors
			oldSuccess, oldErrors = sn, en
			_ = cw.Write([]string{
				strconv.FormatInt(int64(atomic.LoadInt32(&connected)), 10),
				strconv.FormatInt(int64(dsn), 10),
				strconv.FormatInt(int64(den), 10),
			})
		}
		close(done)
		wg.Wait()
		sn, en := atomic.LoadInt32(&success), atomic.LoadInt32(&errors)
		fmt.Println("total:", sn+en)
		fmt.Printf("success: %d (%.0f%%)\n", sn, float64(sn)/float64(sn+en)*100)
		fmt.Printf("errors: %d (%.0f%%)\n", en, float64(en)/float64(sn+en)*100)
		fmt.Println("max:", atomic.LoadInt32(&max))
		return nil
	}
}
