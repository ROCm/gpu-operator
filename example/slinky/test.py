# run this command to check if the GPUs are available
# srun -N 2 --gpus=16 -t 00:02:00 python3 test.py
import torch
 
if torch.cuda.is_available():
    print(f"GPUs available: {torch.cuda.device_count()}")
    for i in range(torch.cuda.device_count()):
        print(f" - GPU {i}: {torch.cuda.get_device_name(i)}")
        print(f" - GPU {i} Pytorch and rocm version: {torch.__version__}")
        print(f" - GPU {i} Nccl version: {torch.cuda.nccl.version()}")
else:
    print("No GPUs available.")
