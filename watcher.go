package vnstat

import (
	"context"
	"fmt"
	"github.com/dlclark/regexp2"
	"log"
	"os/exec"
	"strconv"
	"time"
)

type Traffic struct {
	Time      time.Time
	RxTraffic float64
	RxPacket  uint64
	TxTraffic float64
	TxPacket  uint64
}

type receiver struct {
	regex        *regexp2.Regexp
	ch           chan<- Traffic
	unitSize     float64
	previousTime time.Time
	duration     time.Duration
}

func newReceiver(duration time.Duration, bufferSize int, unitSize float64) (*receiver, <-chan Traffic, error) {
	const pattern = `rx:\s*([\d.]+)\s*(G|M|k)?bit/s.*\s*(\d+)\s*p/s\s*tx:\s*([\d.]+)\s*(G|M|k)?bit/s.*\s*(\d+)\s*p/s`
	regex, err := regexp2.Compile(pattern, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to compile regex: %w", err)
	}

	ch := make(chan Traffic, bufferSize)
	return &receiver{
		regex:    regex,
		ch:       ch,
		unitSize: unitSize,
		duration: duration,
	}, ch, nil
}

func (r *receiver) Write(p []byte) (n int, err error) {
	now := time.Now()

	if now.Sub(r.previousTime) < r.duration {
		return len(p), nil
	}

	defer func() {
		r.previousTime = now
	}()
	
	rxTf, txTf, rxPt, txPt := 0.0, 0.0, uint64(0), uint64(0)
	if m, _ := r.regex.FindStringMatch(string(p)); m != nil {
		groups := m.Groups()

		rxValue, err := strconv.ParseFloat(groups[1].Captures[0].String(), 64)
		if err != nil {
			return 0, fmt.Errorf("failed to parse rxTf value: %w", err)
		}

		switch len(groups[2].Captures) {
		case 0:
			rxTf = rxValue
		default:
			rxUnit := groups[2].Captures[0].String()
			switch rxUnit {
			case "G":
				rxTf = rxValue * 1e9 / r.unitSize
			case "M":
				rxTf = rxValue * 1e6 / r.unitSize
			case "k":
				rxTf = rxValue * 1e3 / r.unitSize
			default:
				rxTf = rxValue / r.unitSize
			}
		}

		rxPacket, err := strconv.ParseUint(groups[3].Captures[0].String(), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("failed to parse rxTf packet: %w", err)
		}
		rxPt = rxPacket

		txValue, err := strconv.ParseFloat(groups[4].Captures[0].String(), 64)
		if err != nil {
			return 0, fmt.Errorf("failed to parse txTf value: %w", err)
		}

		switch len(groups[5].Captures) {
		case 0:
			txTf = txValue
		default:
			txUnit := groups[5].Captures[0].String()
			switch txUnit {
			case "G":
				txTf = txValue * 1e9 / r.unitSize
			case "M":
				txTf = txValue * 1e6 / r.unitSize
			case "k":
				txTf = txValue * 1e3 / r.unitSize
			default:
				txTf = txValue / r.unitSize
			}
		}

		txPacket, err := strconv.ParseUint(groups[6].Captures[0].String(), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("failed to parse txTf packet: %w", err)
		}
		txPt = txPacket
	}

	r.ch <- Traffic{
		Time:      now,
		RxTraffic: rxTf,
		RxPacket:  rxPt,
		TxTraffic: txTf,
		TxPacket:  txPt,
	}

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
