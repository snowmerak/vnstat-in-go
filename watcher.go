package vnstat

import (
	"context"
	"fmt"
	"github.com/dlclark/regexp2"
	"log"
	"os/exec"
	"strconv"
)

type Traffic struct {
	Rx float64
	Tx float64
}

type receiver struct {
	regex    *regexp2.Regexp
	ch       chan<- Traffic
	unitSize float64
}

func newReceiver(bufferSize int, unitSize float64) (*receiver, <-chan Traffic, error) {
	const pattern = `rx:\s*([\d.]+)\s*(G|M|k)?bit/s.*tx:\s*([\d.]+)\s*(G|M|k)?bit/s`
	regex, err := regexp2.Compile(pattern, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to compile regex: %w", err)
	}

	ch := make(chan Traffic, bufferSize)
	return &receiver{
		regex:    regex,
		ch:       ch,
		unitSize: unitSize,
	}, ch, nil
}

func (r *receiver) Write(p []byte) (n int, err error) {
	rx, tx := 0.0, 0.0
	if m, _ := r.regex.FindStringMatch(string(p)); m != nil {
		groups := m.Groups()

		rxValue, err := strconv.ParseFloat(groups[1].Captures[0].String(), 64)
		if err != nil {
			return 0, fmt.Errorf("failed to parse rx value: %w", err)
		}

		switch len(groups[2].Captures) {
		case 0:
			rx = rxValue
		default:
			rxUnit := groups[2].Captures[0].String()
			switch rxUnit {
			case "G":
				rx = rxValue * 1e9 / r.unitSize
			case "M":
				rx = rxValue * 1e6 / r.unitSize
			case "k":
				rx = rxValue * 1e3 / r.unitSize
			default:
				rx = rxValue / r.unitSize
			}
		}

		txValue, err := strconv.ParseFloat(groups[3].Captures[0].String(), 64)
		if err != nil {
			return 0, fmt.Errorf("failed to parse tx value: %w", err)
		}

		switch len(groups[4].Captures) {
		case 0:
			tx = txValue
		default:
			txUnit := groups[4].Captures[0].String()
			switch txUnit {
			case "G":
				tx = txValue * 1e9 / r.unitSize
			case "M":
				tx = txValue * 1e6 / r.unitSize
			case "k":
				tx = txValue * 1e3 / r.unitSize
			default:
				tx = txValue / r.unitSize
			}
		}
	}

	r.ch <- Traffic{Rx: rx, Tx: tx}

	return len(p), nil
}

func (r *receiver) Close() error {
	close(r.ch)
	return nil
}

type Logger interface {
	Printf(format string, v ...interface{})
	Println(v ...interface{})
}

const (
	UnitGbit  = 1e9
	UnitGbyte = 1e9 * 8
	UnitMbit  = 1e6
	UnitMbyte = 1e6 * 8
	UnitKbit  = 1e3
	UnitKbyte = 1e3 * 8
	UnitBit   = 1
	UnitByte  = 8
)

type Watcher struct {
	bufferSize   int
	logger       Logger
	baseUnitSize float64
}

func NewWatcher(ctx context.Context, baseUnitSize float64, bufferSize int, logger Logger) *Watcher {
	if logger == nil {
		logger = log.New(log.Writer(), log.Prefix(), log.Flags())
	}
	return &Watcher{bufferSize: bufferSize, logger: logger, baseUnitSize: baseUnitSize}
}

func (w *Watcher) Watch(ctx context.Context, interfaceName string, callback func(Traffic)) error {
	cmd := exec.Command("vnstat", "-l", "-i", interfaceName)
	rc, ch, err := newReceiver(w.bufferSize, w.baseUnitSize)
	if err != nil {
		return fmt.Errorf("failed to create receiver: %w", err)
	}
	cmd.Stdout = rc

	context.AfterFunc(ctx, func() {
		rc.Close()
		cmd.Cancel()
	})

	go func() {
		if err := cmd.Start(); err != nil {
			w.logger.Printf("failed to start command: %v", err)
			return
		}

		if err := cmd.Wait(); err != nil {
			w.logger.Printf("failed to wait command: %v", err)
			return
		}
	}()

	go func() {
		done := ctx.Done()

		for {
			select {
			case <-done:
				return
			case t := <-ch:
				callback(t)
			}
		}
	}()

	return nil
}
