variable "VERSION" {
    default = "dev"
}

variable "PUSH_ENABLED" {
    default = false
}

group "default" {
    targets = ["ce"]
}

target "ce" {
    dockerfile = "Dockerfile"
    tags = ["jumpserver/koko:${VERSION}-ce"]
    output = PUSH_ENABLED ? ["type=registry"] : ["type=docker"]
}

target "ee" {
    dockerfile = "Dockerfile-ee"
    tags = ["jumpserver/koko:${VERSION}-ee"]
    contexts = {
        "jumpserver/koko:${VERSION}-ce" = "target:ce"
    }
    output = PUSH_ENABLED ? ["type=registry"] : ["type=docker"]
    args = {
        VERSION = "${VERSION}"
    }
    VERSION = "${VERSION}"
}