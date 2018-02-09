package icons

import (
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"

	"github.com/apex/log"
	"github.com/develar/app-builder/fs"
	"github.com/develar/app-builder/util"
	"github.com/disintegration/imaging"
	"github.com/pkg/errors"
)

// returns file if exists, null if file not exists, or error if unknown error
func resolveSourceFileOrNull(sourceFile string, roots []string) (string, os.FileInfo, error) {
	if filepath.IsAbs(sourceFile) {
		cleanPath := filepath.Clean(sourceFile)
		fileInfo, err := os.Stat(cleanPath)
		if err == nil {
			return cleanPath, fileInfo, nil
		}
		return "", nil, errors.WithStack(err)
	}

	for _, root := range roots {
		resolvedPath := filepath.Join(root, sourceFile)
		fileInfo, err := os.Stat(resolvedPath)
		if err == nil {
			return resolvedPath, fileInfo, nil
		} else {
			log.WithFields(log.Fields{
				"path":  resolvedPath,
				"error": err,
			}).Debug("tried resolved path, but got error")
		}
	}

	return "", nil, nil
}

func resolveSourceFile(sourceFiles []string, roots []string, extraExtension string) (string, os.FileInfo, error) {
	for _, sourceFile := range sourceFiles {
		resolvedPath, fileInfo, err := resolveSourceFileOrNull(sourceFile, roots)
		if err != nil {
			return "", nil, errors.WithStack(err)
		}
		if fileInfo != nil {
			return resolvedPath, fileInfo, nil
		}

		if extraExtension != "" {
			var candidate string
			if extraExtension == ".png" && sourceFile == "icons" {
				candidate = "icon.png"
			} else {
				candidate = sourceFile + extraExtension
			}

			resolvedPath, fileInfo, err = resolveSourceFileOrNull(candidate, roots)
			if err != nil {
				return "", nil, errors.WithStack(err)
			}
			if fileInfo != nil {
				return resolvedPath, fileInfo, nil
			}
		}
	}

	return "", nil, errors.Errorf("icon source \"%s\" not found", strings.Join(sourceFiles, ", "))
}

type InputFileInfo struct {
	MaxIconSize int
	MaxIconPath string
	SizeToPath  map[int]string

	maxImage image.Image

	recommendedMinSize int
}

func (t InputFileInfo) GetMaxImage() (image.Image, error) {
	if t.maxImage == nil {
		var err error
		t.maxImage, err = loadImage(t.MaxIconPath, t.recommendedMinSize)
		if err != nil {
			return nil, errors.WithStack(err)
		}
	}
	return t.maxImage, nil
}

func validateImageSize(file string, recommendedMinSize int) error {
	firstFileBytes, err := fs.ReadFile(file, 512)
	if err != nil {
		return errors.WithStack(err)
	}

	if IsIco(firstFileBytes) {
		for _, size := range GetIcoSizes(firstFileBytes) {
			if size.Width >= recommendedMinSize && size.Height >= recommendedMinSize {
				return nil
			}
		}
	} else {
		config, err := DecodeImageConfig(file)
		if err != nil {
			return errors.WithStack(err)
		}

		if config.Width >= recommendedMinSize && config.Height >= recommendedMinSize {
			return nil
		}
	}

	return NewImageSizeError(file, recommendedMinSize)
}

func outputFormatToSingleFileExtension(outputFormat string) string {
	if outputFormat == "set" {
		return ".png"
	}
	return "." + outputFormat
}

func ConvertIcon(sourceFiles []string, roots []string, outputFormat string) ([]IconInfo, error) {
	// allowed to specify path to icns without extension, so, if file not resolved, try to add ".icns" extension
	outExt := outputFormatToSingleFileExtension(outputFormat)
	resolvedPath, fileInfo, err := resolveSourceFile(sourceFiles, roots, outExt)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	var inputInfo InputFileInfo
	inputInfo.SizeToPath = make(map[int]string)

	if outputFormat == "icns" {
		inputInfo.recommendedMinSize = 512
	} else {
		inputInfo.recommendedMinSize = 256
	}

	isOutputFormatIco := outputFormat == "ico"
	if strings.HasSuffix(resolvedPath, outExt) {
		if outputFormat != "icns" {
			err = validateImageSize(resolvedPath, inputInfo.recommendedMinSize)
			if err != nil {
				return nil, errors.WithStack(err)
			}
		}

		// size not required in this case
		return []IconInfo{{File: resolvedPath}}, nil
	}

	if fileInfo.IsDir() {
		icons, err := CollectIcons(resolvedPath)
		if err != nil {
			return nil, errors.WithStack(err)
		}

		if outputFormat == "set" {
			return icons, nil
		}

		for _, file := range icons {
			inputInfo.SizeToPath[file.Size] = file.File
		}

		maxIcon := icons[len(icons)-1]
		inputInfo.MaxIconPath = maxIcon.File
		inputInfo.MaxIconSize = maxIcon.Size
	} else {
		if outputFormat == "set" && strings.HasSuffix(resolvedPath, ".icns") {
			result, err := ConvertIcnsToPng(resolvedPath)
			if err != nil {
				return nil, errors.WithStack(err)
			}
			return result, nil
		}

		maxImage, err := loadImage(resolvedPath, inputInfo.recommendedMinSize)
		if err != nil {
			return nil, errors.WithStack(err)
		}

		if isOutputFormatIco && maxImage.Bounds().Max.X > 256 {
			image256 := imaging.Resize(maxImage, 256, 256, imaging.Lanczos)
			maxImage = image256
		}

		inputInfo.MaxIconSize = maxImage.Bounds().Max.X
		inputInfo.maxImage = maxImage
		inputInfo.SizeToPath[inputInfo.MaxIconSize] = resolvedPath
	}

	switch outputFormat {
	case "icns":
		file, err := ConvertToIcns(inputInfo)
		if err != nil {
			return nil, errors.WithStack(err)
		}
		return []IconInfo{{File: file}}, err

	case "ico":
		maxImage, err := inputInfo.GetMaxImage()
		if err != nil {
			return nil, errors.WithStack(err)
		}

		outFile, err := util.TempFile("", outExt)
		if err != nil {
			return nil, errors.WithStack(err)
		}

		err = SaveImage2(maxImage, outFile, ICO)
		if err != nil {
			return nil, errors.WithStack(err)
		}
		return []IconInfo{{File: outFile.Name()}}, nil

	default:
		return nil, fmt.Errorf("unknown output format %s", resolvedPath)
	}
}

func loadImage(sourceFile string, recommendedMinSize int) (image.Image, error) {
	result, err := LoadImage(sourceFile)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	if result.Bounds().Max.X < recommendedMinSize || result.Bounds().Max.Y < recommendedMinSize {
		return nil, errors.WithStack(NewImageSizeError(sourceFile, recommendedMinSize))
	}

	return result, nil
}
