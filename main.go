package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"
)

const (
	listenAddr = "127.0.0.1:9092"

	// AudioSocket audio format:
	// signed 16-bit PCM, 8000 Hz, mono
	sampleRate    = 8000
	bitsPerSample = 16
	channels      = 1
)

func main() {
	if err := os.MkdirAll("recordings", 0755); err != nil {
		panic(err)
	}

	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		panic(err)
	}
	defer listener.Close()

	fmt.Println("AudioSocket recorder started")
	fmt.Println("Listening:", listenAddr)

	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Println("accept error:", err)
			continue
		}

		fmt.Println("Asterisk connected:", conn.RemoteAddr())

		go handleConnection(conn)
	}
}

func handleConnection(conn net.Conn) {
	defer conn.Close()

	filename := filepath.Join(
		"recordings",
		time.Now().Format("2006-01-02_15-04-05")+".wav",
	)

	file, err := os.Create(filename)
	if err != nil {
		fmt.Println("file create error:", err)
		return
	}
	defer file.Close()

	// WAV header пока записываем с нулевым размером.
	// В конце звонка обновим его настоящими значениями.
	if err := writeWAVHeader(file, 0); err != nil {
		fmt.Println("wav header error:", err)
		return
	}

	var audioBytes uint32

	for {
		frameType, payload, err := readAudioSocketFrame(conn)
		if err != nil {
			if err != io.EOF {
				fmt.Println("AudioSocket error:", err)
			}
			break
		}

		switch frameType {

		// 0x01 = UUID
		case 0x01:
			fmt.Printf("UUID received: %x\n", payload)

		// 0x10 = 8 kHz signed linear PCM
		case 0x10:
			if len(payload) == 0 {
				continue
			}

			if _, err := file.Write(payload); err != nil {
				fmt.Println("audio write error:", err)
				return
			}

			audioBytes += uint32(len(payload))

			fmt.Printf(
				"\rreceived audio: %d bytes",
				audioBytes,
			)

		// 0x00 = hangup
		case 0x00:
			fmt.Println("\nAsterisk closed the call")
			goto finished

		default:
			fmt.Printf(
				"\nUnknown AudioSocket frame type: 0x%02x (%d bytes)\n",
				frameType,
				len(payload),
			)
		}
	}

finished:

	// Обновляем WAV header реальным размером PCM.
	if err := updateWAVHeader(file, audioBytes); err != nil {
		fmt.Println("wav header update error:", err)
		return
	}

	fmt.Printf(
		"Recording saved: %s (%d bytes PCM)\n",
		filename,
		audioBytes,
	)
}

// AudioSocket frame:
//
// +--------+--------+--------+------------------+
// | Type   | Length (2 bytes) | Payload          |
// +--------+--------+--------+------------------+
//
// Type   = 1 byte
// Length = uint16 big-endian
func readAudioSocketFrame(conn net.Conn) (byte, []byte, error) {
	header := make([]byte, 3)

	if _, err := io.ReadFull(conn, header); err != nil {
		return 0, nil, err
	}

	frameType := header[0]

	length := binary.BigEndian.Uint16(header[1:3])

	payload := make([]byte, length)

	if _, err := io.ReadFull(conn, payload); err != nil {
		return 0, nil, err
	}

	return frameType, payload, nil
}

func writeWAVHeader(file *os.File, dataSize uint32) error {
	file.Seek(0, 0)

	byteRate := sampleRate * channels * bitsPerSample / 8
	blockAlign := channels * bitsPerSample / 8

	header := make([]byte, 44)

	copy(header[0:4], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], 36+dataSize)
	copy(header[8:12], "WAVE")

	copy(header[12:16], "fmt ")
	binary.LittleEndian.PutUint32(header[16:20], 16)

	// PCM format
	binary.LittleEndian.PutUint16(header[20:22], 1)

	binary.LittleEndian.PutUint16(header[22:24], channels)
	binary.LittleEndian.PutUint32(header[24:28], sampleRate)
	binary.LittleEndian.PutUint32(header[28:32], uint32(byteRate))
	binary.LittleEndian.PutUint16(header[32:34], uint16(blockAlign))
	binary.LittleEndian.PutUint16(header[34:36], bitsPerSample)

	copy(header[36:40], "data")
	binary.LittleEndian.PutUint32(header[40:44], dataSize)

	_, err := file.Write(header)
	return err
}

func updateWAVHeader(file *os.File, dataSize uint32) error {
	return writeWAVHeader(file, dataSize)
}
