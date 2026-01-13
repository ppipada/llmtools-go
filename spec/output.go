package spec

type ToolStoreOutputKind string

const (
	ToolStoreOutputKindNone  ToolStoreOutputKind = "none"
	ToolStoreOutputKindText  ToolStoreOutputKind = "text"
	ToolStoreOutputKindImage ToolStoreOutputKind = "image"
	ToolStoreOutputKindFile  ToolStoreOutputKind = "file"
)

type ToolStoreOutputText struct {
	Text string `json:"text"`
}

type ImageDetail string

const (
	ImageDetailHigh ImageDetail = "high"
	ImageDetailLow  ImageDetail = "low"
	ImageDetailAuto ImageDetail = "auto"
)

type ToolStoreOutputImage struct {
	Detail    ImageDetail `json:"detail"`
	ImageName string      `json:"imageName"`
	ImageMIME string      `json:"imageMIME"`
	ImageData string      `json:"imageData"`
}

type ToolStoreOutputFile struct {
	FileName string `json:"fileName"`
	FileMIME string `json:"fileMIME"`
	FileData string `json:"fileData"`
}

type ToolStoreOutputUnion struct {
	Kind ToolStoreOutputKind `json:"kind"`

	TextItem  *ToolStoreOutputText  `json:"textItem,omitempty"`
	ImageItem *ToolStoreOutputImage `json:"imageItem,omitempty"`
	FileItem  *ToolStoreOutputFile  `json:"fileItem,omitempty"`
}
