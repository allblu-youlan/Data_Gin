package data_svc

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
)

const oracleExcelRowsPerSheet = 1_000_000

type oracleExcelRows interface {
	Next() bool
	Scan(...interface{}) error
	Err() error
}

func zipOracleExcelFiles(output string, paths []string) error {
	file, err := os.Create(output)
	if err != nil {
		return err
	}
	writer := zip.NewWriter(file)
	for _, path := range paths {
		err := func() error {
			source, err := os.Open(path)
			if err != nil {
				return err
			}
			defer source.Close()
			// XLSX is already compressed; storing avoids a second compression pass.
			target, err := writer.CreateHeader(&zip.FileHeader{Name: filepath.Base(path), Method: zip.Store})
			if err != nil {
				return err
			}
			_, err = io.Copy(target, source)
			return err
		}()
		if err != nil {
			_ = writer.Close()
			_ = file.Close()
			return err
		}
	}
	if err := writer.Close(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	for _, path := range paths {
		_ = os.Remove(path)
	}
	return nil
}
