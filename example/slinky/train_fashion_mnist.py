import os

# Set the Torch Distributed env variables so the training function can be run locally in the Notebook.
# See https://pytorch.org/docs/stable/elastic/run.html#environment-variables
os.environ["RANK"] = "0"
os.environ["LOCAL_RANK"] = "0"
os.environ["WORLD_SIZE"] = "1"
os.environ["MASTER_ADDR"] = "localhost"
os.environ["MASTER_PORT"] = "1234"

def train_fashion_mnist():
    import torch
    import torch.distributed as dist
    import torch.nn.functional as F
    from torch import nn
    from torch.utils.data import DataLoader, DistributedSampler
    from torchvision import datasets, transforms

    # Define the PyTorch CNN model to be trained
    class Net(nn.Module):
        def __init__(self):
            super(Net, self).__init__()
            self.conv1 = nn.Conv2d(1, 20, 5, 1)
            self.conv2 = nn.Conv2d(20, 50, 5, 1)
            self.fc1 = nn.Linear(4 * 4 * 50, 500)
            self.fc2 = nn.Linear(500, 10)

        def forward(self, x):
            x = F.relu(self.conv1(x))
            x = F.max_pool2d(x, 2, 2)
            x = F.relu(self.conv2(x))
            x = F.max_pool2d(x, 2, 2)
            x = x.view(-1, 4 * 4 * 50)
            x = F.relu(self.fc1(x))
            x = self.fc2(x)
            return F.log_softmax(x, dim=1)

    # Use NCCL if a GPU is available, otherwise use Gloo as communication backend.
    device, backend = ("cuda", "nccl") if torch.cuda.is_available() else ("cpu", "gloo")
    print(f"Using Device: {device}, Backend: {backend}")

    # Setup PyTorch distributed.
    local_rank = int(os.getenv("LOCAL_RANK", 0))
    dist.init_process_group(backend=backend)
    print(
        "Distributed Training for WORLD_SIZE: {}, RANK: {}, LOCAL_RANK: {}".format(
            dist.get_world_size(),
            dist.get_rank(),
            local_rank,
        )
    )

    # Create the model and load it into the device.
    device = torch.device(f"{device}:{local_rank}")
    model = nn.parallel.DistributedDataParallel(Net().to(device))
    optimizer = torch.optim.SGD(model.parameters(), lr=0.1, momentum=0.9)

    
    # Download FashionMNIST dataset only on local_rank=0 process.
    if local_rank == 0:
        dataset = datasets.FashionMNIST(
            "./data",
            train=True,
            download=True,
            transform=transforms.Compose([transforms.ToTensor()]),
        )
    dist.barrier()
    dataset = datasets.FashionMNIST(
        "./data",
        train=True,
        download=False,
        transform=transforms.Compose([transforms.ToTensor()]),
    )


    # Shard the dataset accross workers.
    train_loader = DataLoader(
        dataset,
        batch_size=100,
        sampler=DistributedSampler(dataset)
    )

    # TODO(astefanutti): add parameters to the training function
    dist.barrier()
    for epoch in range(1, 10):
        model.train()

        # Iterate over mini-batches from the training set
        for batch_idx, (inputs, labels) in enumerate(train_loader):
            # Copy the data to the GPU device if available
            inputs, labels = inputs.to(device), labels.to(device)
            # Forward pass
            outputs = model(inputs)
            loss = F.nll_loss(outputs, labels)
            # Backward pass
            optimizer.zero_grad()
            loss.backward()
            optimizer.step()

            if batch_idx % 10 == 0 and dist.get_rank() == 0:
                print(
                    "Train Epoch: {} [{}/{} ({:.0f}%)]\tLoss: {:.6f}".format(
                        epoch,
                        batch_idx * len(inputs),
                        len(train_loader.dataset),
                        100.0 * batch_idx / len(train_loader),
                        loss.item(),
                    )
                )

    # Wait for the distributed training to complete
    dist.barrier()
    if dist.get_rank() == 0:
        print("Training is finished")

    # Finally clean up PyTorch distributed
    dist.destroy_process_group()

# Run the training function locally.
train_fashion_mnist()
