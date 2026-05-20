package main

type Maintenance struct {
	ID      string  `json:"id" yaml:"id"`
	Name    string  `json:"name" yaml:"name"`
	Active  bool    `json:"active" yaml:"active"`
	Command string  `json:"command" yaml:"command"`
	Host    SSHHost `json:"host" yaml:"host"`
	Bastion SSHHost `json:"bastion" yaml:"bastion"`
}

type SSHHost struct {
	Address string  `json:"address" yaml:"address"`
	Port    int     `json:"port" yaml:"port"`
	User    string  `json:"user" yaml:"user"`
	Auth    SSHAuth `json:"auth" yaml:"auth"`
}

type SSHAuth struct {
	Password             string `json:"password,omitempty" yaml:"password,omitempty"`
	PrivateKeyPath       string `json:"private_key_path,omitempty" yaml:"private_key_path,omitempty"`
	PrivateKeyPassphrase string `json:"private_key_passphrase,omitempty" yaml:"private_key_passphrase,omitempty"`
}
