package main

import (
	"errors"
	"fmt"
	"github.com/kkdai/youtube/v2"
	"io"
	"log"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Println("Usage: lecture_download url output")
		return
	}
	filePath := os.Args[1]
	output := os.Args[2]
	_, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		log.Fatal("file does not exist")
	} else {
		data, err := os.ReadFile(os.Args[1])
		if err != nil {
			log.Fatal(err.Error())
		}
		youtubePattern, _ := regexp.Compile("https://www.youtube.com/[^\"|\\\\]+")
		links := youtubePattern.FindAllString(string(data), -1)
		for _, link := range links {
			link = strings.Replace(link, "\n", "", -1)
		}
		deduplicatedLinks := deduplicate(links)

		// Get the current date and time
		currentTime := time.Now()

		// Format the date for use as a filename (e.g., "2024-09-01")
		folderName := currentTime.Format("2006-01-02")

		if _, err := os.Stat(folderName); os.IsNotExist(err) {
			// Create the folder
			err = os.Mkdir(folderName, 0755)
			if err != nil {
				log.Fatal("Error creating directory:", err)
			}
		}

		filenames := make([]string, 0)
		for index, link := range deduplicatedLinks {
			filename := folderName + "/" + strconv.Itoa(index) + ".mp4"
			filenames = append(filenames, filename)
			err := downloadVideo(link, filename)
			if err != nil {
				log.Fatal("Error downloading video:", err)
			}
		}
		if err := combineAndBoostVolume(filenames, folderName, output, 5); err != nil {
			log.Fatalf("Error: %v", err)
		}
	}
}

func extractId(link string) (string, error) {
	idPattern, _ := regexp.Compile("watch\\?v=([^\\\\]+)")

	match := idPattern.FindStringSubmatch(link)
	if len(match) > 1 {
		group := match[1]
		return group, nil
	} else {
		return "", errors.New("cannot extract id")
	}

}

func deduplicate(input []string) []string {
	deduplicated := make([]string, 0)
	for _, str := range input {
		if slices.Contains(deduplicated, str) {
			continue
		}
		deduplicated = append(deduplicated, str)
	}
	return deduplicated
}

func downloadVideo(link string, filename string) error {
	videoID, err := extractId(link)
	if err != nil {
		return err
	}
	client := youtube.Client{}

	video, err := client.GetVideo(videoID)
	if err != nil {
		panic(err)
	}

	formats := video.Formats.WithAudioChannels() // only get videos with audio
	formats.Sort()
	stream, _, err := client.GetStream(video, &formats[0])
	if err != nil {
		panic(err)
	}
	defer stream.Close()

	file, err := os.Create(filename)
	if err != nil {
		panic(err)
	}
	defer file.Close()

	_, err = io.Copy(file, stream)
	if err != nil {
		panic(err)
	}
	return nil
}

// normalizeAndBoostAudio boosts the volume of the audio in a video file by a given scale factor.
func normalizeAndBoostAudio(inputFile, outputFile string, volumeScale int) error {
	// Boost the audio volume without normalizing in this step
	volumeArg := fmt.Sprintf("volume=%d", volumeScale)
	cmd := exec.Command("ffmpeg", "-i", inputFile, "-c:v", "libx264", "-vf", "fps=30", "-af", volumeArg, "-c:a", "aac", "-strict", "experimental", "-b:a", "192k", outputFile)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// applyDynaudnorm applies dynamic audio normalization to the final combined video.
func applyDynaudnorm(inputFile, outputFile string) error {
	// Apply dynamic audio normalization to the combined video
	cmd := exec.Command("ffmpeg", "-i", inputFile, "-c:v", "copy", "-af", "dynaudnorm", "-c:a", "aac", "-strict", "experimental", "-b:a", "192k", outputFile)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// combineVideos combines multiple videos into a single video using the ffmpeg concat demuxer.
// filenames: slice of video file paths to combine
func combineVideos(filenames []string, outputFile string) error {
	// Create a temporary file list for concatenation
	fileList := "videos.txt"
	file, err := os.Create(fileList)
	if err != nil {
		return fmt.Errorf("could not create file list: %v", err)
	}
	defer file.Close()

	// Write all filenames to the temporary file list
	for _, filename := range filenames {
		_, err := file.WriteString(fmt.Sprintf("file '%s'\n", filename))
		if err != nil {
			return fmt.Errorf("could not write to file list: %v", err)
		}
	}

	// Combine videos using ffmpeg concat demuxer
	concatCmd := exec.Command("ffmpeg", "-f", "concat", "-safe", "0", "-i", fileList, "-c:v", "libx264", "-c:a", "aac", "-strict", "experimental", "-b:a", "192k", outputFile)
	concatCmd.Stderr = os.Stderr
	if err := concatCmd.Run(); err != nil {
		return fmt.Errorf("error combining videos: %v", err)
	}

	return nil
}

// combineAndBoostVolume processes multiple videos by boosting audio and combining them, then applies dynamic normalization.
// filenames: slice of video file paths to process
// volumeScale: scale factor to increase the volume (e.g., 2 to double the volume)
func combineAndBoostVolume(filenames []string, folder string, output string, volumeScale int) error {
	boostedFiles := make([]string, len(filenames))

	// Boost audio for each video file
	for i, filename := range filenames {
		boostedFile := fmt.Sprintf("%s/boosted_%d.mp4", folder, i)

		if err := normalizeAndBoostAudio(filename, boostedFile, volumeScale); err != nil {
			return fmt.Errorf("error boosting audio for video %s: %v", filename, err)
		}

		boostedFiles[i] = boostedFile
	}

	// Combine all boosted videos into one final output
	combinedOutput := "combined_output.mp4"
	if err := combineVideos(boostedFiles, combinedOutput); err != nil {
		return fmt.Errorf("error combining videos: %v", err)
	}

	// Apply dynamic audio normalization to the combined video
	if err := applyDynaudnorm(combinedOutput, output); err != nil {
		return fmt.Errorf("error normalizing audio: %v", err)
	}

	fmt.Printf("Successfully created output file: %s\n", output)
	return nil
}
