package main

//kudos to https://github.com/beevik/ntp/blob/master/ntp.go for inspiration

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"time"
)

const (
	defaultNtpVersion = 4
	defaultTimeout    = 15 * time.Second
)

var (
	ntpEpochOffset = time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC).Unix() - time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
)

type packet struct {
	Settings       uint8  // leap yr indicator, ver number, and mode
	Stratum        uint8  // stratum of local clock
	Poll           int8   // poll exponent
	Precision      int8   // precision exponent
	RootDelay      uint32 // root delay
	RootDispersion uint32 // root dispersion
	ReferenceID    uint32 // reference id
	RefTimeSec     uint32 // reference timestamp sec
	RefTimeFrac    uint32 // reference timestamp fractional
	OrigTimeSec    uint32 // origin time secs
	OrigTimeFrac   uint32 // origin time fractional
	RxTimeSec      uint32 // receive time secs
	RxTimeFrac     uint32 // receive time frac
	TxTimeSec      uint32 // transmit time secs
	TxTimeFrac     uint32 // transmit time frac
}

func main() {
	start := time.Now()

	// Wall clock reading
	t := time.Now()
	// Monotonic clock reading
	elapsed := t.Sub(start)
	fmt.Printf("Start time %d elapsed %d\n", start, elapsed)
	fmt.Printf("Local %s\n Standard (ISO 8601) time %s\n UTC time %s\n Unix time %d\n", t.Local().String(), t, t.UTC(), t.Unix())

	fmt.Println("Getting time from NTP server ... ")
	msg, ntp, err := getTime("dk.pool.ntp.org")
	if err != nil {
		panic(err)
	}

	fmt.Printf("Msg %+v\nNTP time %d as Unix time \n.....", msg, ntp)
}

func getTime(host string) (*packet, int64, error) {
	radr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(host, "123"))
	var ladr *net.UDPAddr
	if err != nil {
		return nil, 0, err
	}
	conn, err := net.DialUDP("udp", ladr, radr)
	if err != nil {
		return nil, 0, err
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(defaultTimeout))

	req := &packet{Settings: 0x1B}
	xmitTime := time.Now()

	// query NTP
	err = binary.Write(conn, binary.BigEndian, req)
	if err != nil {
		return nil, 0, err
	}
	// Receive the response.
	rsp := new(packet)
	err = binary.Read(conn, binary.BigEndian, rsp)
	if err != nil {
		return nil, 0, err
	}
	delta := time.Since(xmitTime)
	if delta < 0 {
		// The local system may have had its clock adjusted since it
		// sent the query. In go 1.9 and later, time.Since ensures
		// that a monotonic clock is used, so delta can never be less
		// than zero. In versions before 1.9, a monotonic clock is
		// not used, so we have to check.
		return nil, 0, errors.New("client clock ticked backwards")
	}

	secs := int64(rsp.TxTimeSec) - ntpEpochOffset

	return rsp, secs, nil

}
