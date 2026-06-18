package ai

import (
	"fmt"

	onnxruntime_go "github.com/yalue/onnxruntime_go"
)

type ONNXEngine struct {
	session   *onnxruntime_go.DynamicSession
	inputDim  int
	outputDim int
}

func NewONNXEngine(modelPath string, inputDim, outputDim int) (*ONNXEngine, error) {
	if inputDim <= 0 || outputDim <= 0 {
		return nil, fmt.Errorf("invalid input/output dimensions: %d/%d", inputDim, outputDim)
	}

	onnxruntime_go.SetSharedLibraryPath(onnxruntime_go.GetDefaultSharedLibraryPath())
	if err := onnxruntime_go.InitializeEnvironment(); err != nil {
		return nil, fmt.Errorf("failed to initialize onnxruntime environment: %w", err)
	}

	sessionOptions := onnxruntime_go.NewSessionOptions()
	defer sessionOptions.Destroy()

	sessionOptions.SetIntraOpNumThreads(1)
	sessionOptions.SetGraphOptimizationLevel(onnxruntime_go.GraphOptimizationLevelORT_ENABLE_ALL)

	session, err := onnxruntime_go.NewDynamicSession(modelPath, sessionOptions)
	if err != nil {
		onnxruntime_go.DestroyEnvironment()
		return nil, fmt.Errorf("failed to load onnx model %s: %w", modelPath, err)
	}

	return &ONNXEngine{
		session:   session,
		inputDim:  inputDim,
		outputDim: outputDim,
	}, nil
}

func (e *ONNXEngine) RunInference(input []float32) ([]float32, error) {
	if e.session == nil {
		return nil, fmt.Errorf("onnx session is nil")
	}
	if len(input) != e.inputDim {
		return nil, fmt.Errorf("input dimension mismatch: expected %d, got %d", e.inputDim, len(input))
	}

	inputShape := []int64{1, int64(e.inputDim)}
	outputShape := []int64{1, int64(e.outputDim)}

	inputTensor, err := onnxruntime_go.NewTensor(inputShape, input)
	if err != nil {
		return nil, fmt.Errorf("failed to create input tensor: %w", err)
	}
	defer inputTensor.Destroy()

	outputTensor, err := onnxruntime_go.NewEmptyTensor[float32](outputShape)
	if err != nil {
		return nil, fmt.Errorf("failed to create output tensor: %w", err)
	}
	defer outputTensor.Destroy()

	inputNames := e.session.InputNames()
	outputNames := e.session.OutputNames()

	if len(inputNames) == 0 || len(outputNames) == 0 {
		return nil, fmt.Errorf("model has no input or output names")
	}

	err = e.session.Run(
		[]string{inputNames[0]},
		[]onnxruntime_go.Value{inputTensor},
		[]string{outputNames[0]},
		[]onnxruntime_go.Value{outputTensor},
	)
	if err != nil {
		return nil, fmt.Errorf("inference failed: %w", err)
	}

	outputData := outputTensor.GetData()
	result := make([]float32, len(outputData))
	copy(result, outputData)

	return result, nil
}

func (e *ONNXEngine) Close() {
	if e.session != nil {
		e.session.Destroy()
		e.session = nil
	}
	onnxruntime_go.DestroyEnvironment()
}
